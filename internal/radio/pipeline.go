package radio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/learning"
	"catchup-feed/internal/repository"
	"catchup-feed/internal/script"
	"catchup-feed/internal/tts"
)

// ErrNoArticles is returned when no summarized article arrived since the
// previous episode. D-1: 記事ゼロの日はスキップ — the caller treats this as
// a clean no-op, not a failure.
var ErrNoArticles = errors.New("radio: no new summarized articles, skipping episode")

// selectionLimit bounds the backlog fetched per run. Overflow beyond the
// on-air cap still reaches the show notes; a multi-week gap is truncated
// rather than producing a book-sized description.
const selectionLimit = 200

// ArticleSource selects the episode's articles (§6-1).
type ArticleSource interface {
	ListSummarizedSince(ctx context.Context, since time.Time, limit int) ([]repository.RadioArticle, error)
}

// EpisodeStore is the episode persistence the pipeline needs: the previous
// episode (selection cursor), today's count (rev numbering) and the final
// insert. Satisfied by repository.EpisodeRepository.
type EpisodeStore interface {
	ListByKind(ctx context.Context, feedKind string, limit int) ([]*entity.Episode, error)
	CountByKindSince(ctx context.Context, feedKind string, since time.Time) (int, error)
	Create(ctx context.Context, episode *entity.Episode, segments []*entity.Segment) error
}

// JobQueue enqueues the follow-up work for the worker on the Pi (§6-5 /
// C-4). Satisfied by repository.JobRepository.
type JobQueue interface {
	Enqueue(ctx context.Context, kind string, payload json.RawMessage, runAfter time.Time) (int64, error)
}

// ScriptGenerator turns planned articles into segments (§6-2). Satisfied
// by script.Generator. quizCount > 0 additionally piggybacks the Phase 3
// learning-item request on the same LLM calls (D-19) and returns the
// parsed drafts; quiz-side failures degrade to nil drafts, never to an
// error (Phase 3 §5.1).
type ScriptGenerator interface {
	GenerateEpisode(ctx context.Context, date time.Time, articles []repository.RadioArticle, quizCount int) ([]*entity.Segment, []script.QuizDraft, error)
}

// LearningStore is the Phase 3 learning-item side of the batch (§5.1/
// §5.2): the backpressure input, the same-day dedupe and the insert sink.
// Satisfied by repository.LearningRepository. A nil LearningStore disables
// item generation entirely — the broadcast pipeline itself never depends
// on it (§12-1: 公開エピソードの完全不変).
type LearningStore interface {
	CountOverdueActive(ctx context.Context, day time.Time) (int, error)
	HasArticleItemCreatedOn(ctx context.Context, day time.Time) (bool, error)
	InsertItem(ctx context.Context, item learning.NewItem, dueOn time.Time) (int64, error)
}

// Synthesizer renders one segment script as sentence WAVs (§6-3) and names
// the voice it uses for the mandatory VOICEVOX credit (U-13). Satisfied by
// tts.Voicevox.
type Synthesizer interface {
	SynthesizeScript(ctx context.Context, script string) ([]tts.Audio, error)
	// SpeakerName resolves the credit speaker name for the configured
	// voice. An error aborts the run — episodes must never ship without
	// their 「VOICEVOX:話者名」 credit.
	SpeakerName(ctx context.Context) (string, error)
}

// Encoder produces the final mp3 (§6-4). Satisfied by tts.FFmpeg.
type Encoder interface {
	ConcatToMP3(ctx context.Context, wavPaths []string, outPath string, tags tts.ID3) error
}

// RunOptions tweak a single pipeline run.
type RunOptions struct {
	// DryRun stops after script generation and prints the would-be episode
	// to Out — TTS, encoding, transfer and every DB write are skipped
	// (D-2: 話者選定・プロンプト調整用).
	DryRun bool
	// Since overrides the selection cursor (default: previous public
	// episode's published_at). Useful for manual re-runs.
	Since *time.Time
}

// Pipeline is the §6 episode batch. All intermediate artifacts live in a
// temp dir and the DB is written only after the mp3 sits on the Pi (§6-6:
// 生成途中の失敗はテンポラリディレクトリ内で完結し、DB には成功時のみ書き込む).
type Pipeline struct {
	Articles ArticleSource
	Episodes EpisodeStore
	Jobs     JobQueue
	Script   ScriptGenerator
	TTS      Synthesizer
	Encoder  Encoder
	Transfer Transferer
	// Learning enables Phase 3 item generation (§5.1); nil turns it off.
	Learning LearningStore
	// LearningCfg carries the D-18 quiz parameters (QUIZ_* env). Only
	// ItemsPerDay and BackpressureThreshold are read here.
	LearningCfg learning.Config
	Config      Config
	Logger      *slog.Logger
	Now         func() time.Time // nil = time.Now
	Out         io.Writer        // dry-run output; nil = os.Stdout
}

// Run executes one episode generation. It returns ErrNoArticles on an
// empty day (D-1: skip); any other error means the day is skipped as a
// failure and launchd retries tomorrow (§8).
func (p *Pipeline) Run(ctx context.Context, opts RunOptions) error {
	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}
	loc := p.Config.Location
	if loc == nil { // LoadConfig always sets it; guard direct construction
		loc = time.Local
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	now = now.In(loc)

	// --- §6-1 記事選定 ---
	// `now` doubles as the selection timestamp and, later, as the episode's
	// explicit published_at. The next run's cursor therefore equals the
	// moment this SELECT ran: a summary the worker inserts while this batch
	// is still generating (created_at > now) falls inside the next run's
	// window instead of vanishing between SELECT and INSERT. The overlap
	// this leans into is harmless — already-broadcast articles are excluded
	// structurally (ListSummarizedSince's NOT EXISTS on segments).
	since, err := p.selectionCursor(ctx, opts)
	if err != nil {
		return err
	}
	articles, err := p.Articles.ListSummarizedSince(ctx, since, selectionLimit)
	if err != nil {
		return fmt.Errorf("radio: select articles: %w", err)
	}
	if len(articles) == 0 {
		return ErrNoArticles
	}
	featured, overflow := script.Plan(articles, p.Config.MaxArticles)
	logger.Info("articles selected",
		slog.Time("since", since),
		slog.Int("featured", len(featured)),
		slog.Int("overflow", len(overflow)))

	// --- U-13 VOICEVOX クレジット ---
	// Resolved before the LLM stage so a dead engine fails fast instead of
	// burning free-tier quota on a script that cannot be voiced anyway. A
	// resolution failure aborts the run: shipping an episode without its
	// 「VOICEVOX:話者名」 credit would violate the VOICEVOX terms of use.
	// Dry-run keeps going with a placeholder — it never distributes audio
	// and must stay usable on machines without the engine (D-2).
	speakerName, err := p.TTS.SpeakerName(ctx)
	if err != nil {
		if !opts.DryRun {
			return fmt.Errorf("radio: resolve VOICEVOX speaker name for credit (U-13): %w", err)
		}
		logger.Warn("VOICEVOX speaker name unresolved, dry-run uses a placeholder", slog.Any("error", err))
		speakerName = "(話者名未解決)"
	}

	// --- §6-2 台本生成(Phase 3 §5.1: 学習項目の相乗り、D-19) ---
	// quizCount is decided BEFORE the LLM call so that backpressure and the
	// same-day dedupe suppress the prompt section itself — no tokens spent,
	// no output discarded (§5.2). The decision inputs (overdue counts,
	// existing items) stay on this side of the call and never reach the
	// prompt (§10: 理解状態をクラウドに漏らさない).
	quizCount := p.newItemQuota(ctx, now, logger)
	segments, quizDrafts, err := p.Script.GenerateEpisode(ctx, now, featured, quizCount)
	if err != nil {
		return fmt.Errorf("radio: generate script: %w", err)
	}
	showNotes := script.AppendVoicevoxCredit(script.BuildShowNotes(featured, overflow), speakerName)

	// --- §6-6 冪等性: 同日再実行は rev 付き新規版 ---
	title, filename, err := p.episodeNaming(ctx, now)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return p.printDryRun(title, since, showNotes, segments, quizDrafts)
	}

	tmpDir, err := os.MkdirTemp("", "radio-episode-")
	if err != nil {
		return fmt.Errorf("radio: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// --- §6-3 TTS ---
	wavPaths, totalDuration, err := p.synthesize(ctx, tmpDir, segments)
	if err != nil {
		return err // VOICEVOX 障害→当日スキップ (§8)
	}

	// --- §6-4 結合 ---
	mp3Path := filepath.Join(tmpDir, filename)
	tags := tts.ID3{
		Title:  title,
		Artist: p.Config.ShowName,
		Album:  p.Config.ShowName,
		Date:   now.Format("2006-01-02"),
	}
	if err := p.Encoder.ConcatToMP3(ctx, wavPaths, mp3Path, tags); err != nil {
		return fmt.Errorf("radio: encode: %w", err)
	}
	stat, err := os.Stat(mp3Path)
	if err != nil {
		return fmt.Errorf("radio: stat mp3: %w", err)
	}

	// --- §6-5 転送・登録 ---
	audioPath, err := p.Transfer.Transfer(ctx, mp3Path, filename)
	if err != nil {
		return err
	}
	episode := &entity.Episode{
		FeedKind:    entity.FeedKindPublic,
		Title:       title,
		ShowNotes:   showNotes,
		AudioPath:   audioPath,
		AudioBytes:  stat.Size(),
		DurationSec: int(math.Round(totalDuration.Seconds())),
		// The selection timestamp, not the INSERT time: this becomes the
		// next run's cursor, closing the SELECT-to-INSERT window in which
		// the worker may have summarized more articles (see above).
		PublishedAt: now,
	}
	if err := p.Episodes.Create(ctx, episode, segments); err != nil {
		return fmt.Errorf("radio: insert episode: %w", err)
	}
	if _, err := p.Jobs.Enqueue(ctx, entity.JobKindRegenerateFeed, nil, time.Time{}); err != nil {
		return fmt.Errorf("radio: enqueue regenerate_feed: %w", err)
	}
	notifyPayload, err := json.Marshal(map[string]int64{"episode_id": episode.ID})
	if err != nil {
		return fmt.Errorf("radio: marshal notify payload: %w", err)
	}
	if _, err := p.Jobs.Enqueue(ctx, entity.JobKindNotifyEpisode, notifyPayload, time.Time{}); err != nil {
		return fmt.Errorf("radio: enqueue notify_episode: %w", err)
	}

	// --- Phase 3 §5.1 学習項目の登録(best-effort) ---
	// Runs only after the broadcast is fully committed: an aborted run must
	// leave no items (放送されなかった記事は学習対象にしない), and an
	// insert failure must never fail the run (§9). A same-day rev after a
	// partial failure regenerates the drafts; HasArticleItemCreatedOn keeps
	// that from double-inserting.
	p.insertLearningItems(ctx, logger, quizDrafts, now)

	logger.Info("episode generated",
		slog.Int64("episode_id", episode.ID),
		slog.String("title", title),
		slog.String("audio_path", audioPath),
		slog.Int64("audio_bytes", episode.AudioBytes),
		slog.Int("duration_sec", episode.DurationSec),
		slog.Int("segments", len(segments)))
	return nil
}

// newItemQuota decides M for this run (Phase 3 §5.1/§5.2): the configured
// ItemsPerDay, or 0 when generation must be suppressed — no LearningStore,
// backpressure over the threshold (strictly greater: 閾値ちょうどは継続),
// or the day's items already exist (same-day rev re-run, §12-2). Zero
// means the outro prompt carries no learning-item section at all.
//
// Every check failure also degrades to 0 with a warning: item generation
// must never stop or delay the broadcast (§9), and the day is simply
// item-less (遡り生成はしない、§5.2).
func (p *Pipeline) newItemQuota(ctx context.Context, now time.Time, logger *slog.Logger) int {
	if p.Learning == nil || p.LearningCfg.ItemsPerDay <= 0 {
		return 0
	}
	day := learning.BroadcastDay(now)

	overdue, err := p.Learning.CountOverdueActive(ctx, day)
	if err != nil {
		logger.Warn("learning backpressure check failed, skipping item generation (§9)",
			slog.Any("error", err))
		return 0
	}
	if overdue > p.LearningCfg.BackpressureThreshold {
		logger.Warn("learning backlog over threshold, suspending new item generation (§5.2 バックプレッシャ)",
			slog.Int("overdue", overdue),
			slog.Int("threshold", p.LearningCfg.BackpressureThreshold))
		return 0
	}

	exists, err := p.Learning.HasArticleItemCreatedOn(ctx, day)
	if err != nil {
		logger.Warn("learning same-day dedupe check failed, skipping item generation (§9)",
			slog.Any("error", err))
		return 0
	}
	if exists {
		logger.Info("learning items already generated today, skipping (same-day rev re-run, §12-2)",
			slog.String("day", learning.FormatDay(day)))
		return 0
	}
	return p.LearningCfg.ItemsPerDay
}

// insertLearningItems persists the day's parsed drafts (§5.1): kind
// 'article', stage 0, due_on = 翌放送日 — 当日のクイズコーナーには出さない
// (初回想起は翌日). Strictly best-effort: the episode is already on the Pi
// and registered, so a DB error here loses today's items but never the
// broadcast (§9); each failure is logged and the loop continues.
func (p *Pipeline) insertLearningItems(ctx context.Context, logger *slog.Logger, drafts []script.QuizDraft, now time.Time) {
	if p.Learning == nil || len(drafts) == 0 {
		return
	}
	dueOn := learning.FirstDueDay(now)
	for _, draft := range drafts {
		articleID := draft.ArticleID
		item := learning.NewItem{
			Kind:      learning.KindArticle,
			ArticleID: &articleID,
			Concept:   draft.Concept,
			Question:  draft.Question,
			Answer:    draft.Answer,
			Provider:  draft.Provider,
		}
		id, err := p.Learning.InsertItem(ctx, item, dueOn)
		if err != nil {
			logger.Warn("failed to insert learning item, continuing (§5.1: 放送は止めない)",
				slog.Int64("article_id", draft.ArticleID),
				slog.Any("error", err))
			continue
		}
		logger.Info("learning item created",
			slog.Int64("item_id", id),
			slog.Int64("article_id", draft.ArticleID),
			slog.String("provider", draft.Provider),
			slog.String("due_on", learning.FormatDay(dueOn)))
	}
}

// selectionCursor returns the article-selection cursor: an explicit
// override, or the previous public episode's published_at, or the zero time
// on the very first run (§6-1: 前回 public エピソード以降).
func (p *Pipeline) selectionCursor(ctx context.Context, opts RunOptions) (time.Time, error) {
	if opts.Since != nil {
		return *opts.Since, nil
	}
	last, err := p.Episodes.ListByKind(ctx, entity.FeedKindPublic, 1)
	if err != nil {
		return time.Time{}, fmt.Errorf("radio: load previous episode: %w", err)
	}
	if len(last) == 0 {
		return time.Time{}, nil
	}
	return last[0].PublishedAt, nil
}

// episodeNaming derives the episode title and mp3 filename from the
// broadcast day. A same-day re-run never overwrites: the n-th run of the
// day becomes "…revN" with a distinct filename (§6-6).
func (p *Pipeline) episodeNaming(ctx context.Context, now time.Time) (title, filename string, err error) {
	day := now.Format("2006-01-02")
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	count, err := p.Episodes.CountByKindSince(ctx, entity.FeedKindPublic, startOfDay)
	if err != nil {
		return "", "", fmt.Errorf("radio: count today's episodes: %w", err)
	}
	title = fmt.Sprintf("%s %s", p.Config.ShowName, day)
	filename = day + ".mp3"
	if count > 0 {
		rev := count + 1
		title = fmt.Sprintf("%s rev%d", title, rev)
		filename = fmt.Sprintf("%s-rev%d.mp3", day, rev)
	}
	return title, filename, nil
}

// synthesize renders every segment through TTS into wav files inside dir,
// returning the ordered paths and the summed playing time.
func (p *Pipeline) synthesize(ctx context.Context, dir string, segments []*entity.Segment) ([]string, time.Duration, error) {
	var wavPaths []string
	var total time.Duration
	for i, segment := range segments {
		audios, err := p.TTS.SynthesizeScript(ctx, segment.Script)
		if err != nil {
			return nil, 0, fmt.Errorf("radio: tts segment %d (%s): %w", i+1, segment.Kind, err)
		}
		for j, audio := range audios {
			path := filepath.Join(dir, fmt.Sprintf("seg_%03d_%03d.wav", i, j))
			if err := os.WriteFile(path, audio.Data, 0o600); err != nil {
				return nil, 0, fmt.Errorf("radio: write wav: %w", err)
			}
			wavPaths = append(wavPaths, path)
			total += audio.Duration
		}
	}
	return wavPaths, total, nil
}

// printDryRun writes the would-be episode to Out (D-2: 台本を目視で調整).
// The report is the sole product of a dry-run, so it is rendered in memory
// and written once, with the write error surfaced to the caller. Learning
// item drafts are printed for prompt tuning but never inserted — dry-run
// makes no DB writes (Phase 3 手順2).
func (p *Pipeline) printDryRun(title string, since time.Time, showNotes string, segments []*entity.Segment, drafts []script.QuizDraft) error {
	out := p.Out
	if out == nil {
		out = os.Stdout
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "=== dry-run: %s ===\n", title)
	fmt.Fprintf(&sb, "selection since: %s\n\n", since.Format(time.RFC3339))
	fmt.Fprintf(&sb, "--- show notes ---\n%s\n\n", showNotes)
	for _, segment := range segments {
		fmt.Fprintf(&sb, "--- segment %d [%s] ---\n%s\n\n", segment.Position, segment.Kind, segment.Script)
	}
	for i, draft := range drafts {
		fmt.Fprintf(&sb, "--- learning item %d (dry-run, not inserted) [article %d, %s] ---\n概念: %s\n問題: %s\n答え: %s\n\n",
			i+1, draft.ArticleID, draft.Provider, draft.Concept, draft.Question, draft.Answer)
	}
	if _, err := io.WriteString(out, sb.String()); err != nil {
		return fmt.Errorf("radio: write dry-run report: %w", err)
	}
	return nil
}
