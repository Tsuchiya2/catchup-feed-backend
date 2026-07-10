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
// previous episode AND no quiz item is due (Phase 3 §7.1). D-1: 記事ゼロの
// 日はスキップ — the caller treats this as a clean no-op, not a failure.
var ErrNoArticles = errors.New("radio: no new summarized articles, skipping episode")

// selectionLimit bounds the backlog fetched per run. Overflow beyond the
// on-air cap still reaches the show notes; a multi-week gap is truncated
// rather than producing a book-sized description.
const selectionLimit = 200

// quizPause is the silence between a quiz question and its answer (Phase 3
// §7.2: question 読み上げ → 無音3秒 → answer 読み上げ). The pause is a
// standalone silence wav in the ffmpeg concat list — never a VOICEVOX
// "long pause" (§12-5) — fabricated per run in the engine's exact output
// format (see Pipeline.synthesizeQuizCorner).
const quizPause = 3 * time.Second

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

// LearningStore is the Phase 3 learning side of the batch: item generation
// (§5.1/§5.2 — backpressure, same-day dedupe, insert) plus the review loop
// (§3 の実行順: AutoResolve → 生成 → ListDue → 出題記録). Satisfied by
// repository.LearningRepository. A nil LearningStore disables the learning
// loop entirely — item generation, the private twin, everything; the public
// broadcast never depends on it (§12-1: 公開エピソードの完全不変).
type LearningStore interface {
	CountOverdueActive(ctx context.Context, day time.Time) (int, error)
	HasArticleItemCreatedOn(ctx context.Context, day time.Time) (bool, error)
	InsertItem(ctx context.Context, item learning.NewItem, dueOn time.Time) (int64, error)
	AutoResolve(ctx context.Context, cutoffDay, resolveDay time.Time, ladder []int) (int, error)
	ListDue(ctx context.Context, day time.Time, limit int) ([]learning.Item, error)
	RecordAsked(ctx context.Context, itemIDs []int64, episodeID int64, askedOn time.Time) error
	WeeklyReviewMaterial(ctx context.Context, fromDay time.Time, ladderLen int) (learning.WeeklyReview, error)
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
	// Learning enables the Phase 3 learning loop (§5.1/§7); nil turns it
	// off, including the private twin episode.
	Learning LearningStore
	// BookReview enables the §7.3 book_review corner (active book, chunks,
	// cursor); nil (or a nil BookReviewLLM) leaves the private episode
	// news+quiz only. Requires Learning for the §5.3 book quiz.
	BookReview BookReviewStore
	// BookReviewLLM generates the book_review script + book quiz from a LOCAL
	// model only (§12-4); nil disables book_review.
	BookReviewLLM BookReviewer
	// LearningCfg carries the D-17/D-18 quiz parameters (QUIZ_* env).
	LearningCfg learning.Config
	Config      Config
	Logger      *slog.Logger
	Now         func() time.Time // nil = time.Now
	Out         io.Writer        // dry-run output; nil = os.Stdout
}

// Run executes one episode generation. It returns ErrNoArticles on an
// empty day (D-1: skip, unless quiz items are due — then a private-only
// episode ships, Phase 3 §7.1); any other error means the day is skipped
// as a failure and launchd retries tomorrow (§8).
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
		// The public skip contract stays byte-identical (D-1 → ErrNoArticles
		// → exit 0). Phase 3 §7.1/§12-8: the due-item check lives strictly
		// BEHIND this early return so it can never resurrect a public
		// episode — it may only add a private-only one.
		return p.runQuizOnlyDay(ctx, opts, now, since, logger)
	}
	featured, overflow := script.Plan(articles, p.Config.MaxArticles)
	logger.Info("articles selected",
		slog.Time("since", since),
		slog.Int("featured", len(featured)),
		slog.Int("overflow", len(overflow)))

	// --- U-13 VOICEVOX クレジット ---
	// Resolved before the LLM stage so a dead engine fails fast instead of
	// burning free-tier quota on a script that cannot be voiced anyway.
	speakerName, err := p.resolveSpeakerName(ctx, opts.DryRun, logger)
	if err != nil {
		return err
	}

	// --- Phase 3 §3 手順1: 未採点ログの自動解決(48h、D-17) ---
	// Deliberately BEFORE the quota decision and the due selection (§6.1:
	// 自動解決の実行タイミングは選定直前): resolving stale logs both drains
	// the overdue backlog the backpressure check reads and re-schedules the
	// affected items past today.
	p.autoResolve(ctx, now, opts.DryRun, logger)

	// --- §6-2 台本生成(Phase 3 §5.1: 学習項目の相乗り、D-19) ---
	// quizCount is decided BEFORE the LLM call so that backpressure and the
	// same-day dedupe suppress the prompt section itself — no tokens spent,
	// no output discarded (§5.2). The decision inputs (overdue counts,
	// existing items) stay on this side of the call and never reach the
	// prompt (§10: 理解状態をクラウドに漏らさない). The two DB reads behind
	// the decision run in dry-run too — deliberately: both are read-only,
	// and a dry-run must render the outro prompt exactly as the real run
	// would (D-2: プロンプト調整用), backpressure state included.
	quizCount := p.newItemQuota(ctx, now, logger)
	segments, quizDrafts, err := p.Script.GenerateEpisode(ctx, now, featured, quizCount)
	if err != nil {
		return fmt.Errorf("radio: generate script: %w", err)
	}

	// --- Phase 3 §6.3 出題選定 ---
	// Read-only, so it runs in dry-run too and is printed for inspection.
	// A selection failure degrades the private twin to news-only (§9) —
	// the public side is untouched by construction.
	dueItems := p.listDueItems(ctx, now, logger)
	corner := script.BuildQuizCorner(dueItems)

	baseNotes := script.BuildShowNotes(featured, overflow)
	showNotes := script.AppendVoicevoxCredit(baseNotes, speakerName)

	// --- §6-6 冪等性: 同日再実行は rev 付き新規版 ---
	title, filename, err := p.episodeNaming(ctx, now, entity.FeedKindPublic)
	if err != nil {
		return err
	}

	if opts.DryRun {
		brSel := p.selectBookReview(ctx, logger, now, true)
		reviewMat := p.previewWeeklyReview(ctx, logger, now)
		return p.printDryRun(title, since, showNotes, segments, quizDrafts, dueItems, brSel, reviewMat)
	}

	tmpDir, err := os.MkdirTemp("", "radio-episode-")
	if err != nil {
		return fmt.Errorf("radio: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// --- §6-3 TTS ---
	segWavs, totalDuration, err := p.synthesize(ctx, tmpDir, segments)
	if err != nil {
		return err // VOICEVOX 障害→当日スキップ (§8)
	}

	// --- §6-4 結合 ---
	// The public concat list is exactly the synthesized segments, in order
	// — no quiz material can appear here by construction (§12-1: 公開
	// エピソードの構成・尺は完全不変).
	mp3Path := filepath.Join(tmpDir, filename)
	tags := tts.ID3{
		Title:  title,
		Artist: p.Config.ShowName,
		Album:  p.Config.ShowName,
		Date:   now.Format("2006-01-02"),
	}
	if err := p.Encoder.ConcatToMP3(ctx, flattenWavs(segWavs), mp3Path, tags); err != nil {
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
	// 契約 (Phase 3 §12-7): notify_episode は公開エピソードに対して**のみ**
	// 積む。私的版は「積んで無視」ではなく「積まない」— NotifyEpisodeHandler
	// は feed_kind に依らず管理チャネル(Discord/Slack)へ show_notes を送る
	// ため、学習コンテンツ(復習 concept 一覧)を含む私的版のジョブが存在
	// した時点で §10(学習コンテンツを外部サービスに流さない)に違反する。
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

	// --- Phase 3 §7.1 私的エピソード(二本立て、best-effort) ---
	// Strictly AFTER the public episode is committed: a private-side
	// failure logs and gives up the twin only — 縮退方向は「公開版は出す、
	// 私的版のみ諦める」. The reverse needs no code: any public failure
	// returns above, so a private episode can never outlive a failed
	// public one (news wav 共用).
	p.publishPrivateEpisode(ctx, logger, privateEpisodeInput{
		tmpDir:       tmpDir,
		now:          now,
		segments:     segments,
		segWavs:      segWavs,
		newsDuration: totalDuration,
		dueItems:     dueItems,
		corner:       corner,
		baseNotes:    baseNotes,
		speakerName:  speakerName,
	})
	return nil
}

// runQuizOnlyDay handles the no-articles morning (Phase 3 §7.1/§12-8): the
// public episode is skipped exactly as before (D-1), but when quiz items are
// due OR a book_review is in progress a private-only episode ships — fixed
// intro → quiz corner → book_review → fixed outro, no news. The quiz corner is
// template-only (クオータ消費ゼロ、§10 によりクイズ内容はクラウドに送れない);
// the book_review rides on the local model only (§12-4). No news means no
// length pressure, so the §7.1 18-minute guard does not apply here. Unlike the
// news-day twin this episode IS the run's whole product, so failures return an
// error and the admin gets the notify_error notice (§8).
func (p *Pipeline) runQuizOnlyDay(ctx context.Context, opts RunOptions, now, since time.Time, logger *slog.Logger) error {
	if p.Learning == nil {
		return ErrNoArticles
	}
	p.autoResolve(ctx, now, opts.DryRun, logger)
	dueItems := p.listDueItems(ctx, now, logger)
	brSel := p.selectBookReview(ctx, logger, now, opts.DryRun)
	if len(dueItems) == 0 && brSel == nil {
		// 記事も期日到来項目もアクティブ書籍もない → 従来どおりスキップ (D-1)。
		return ErrNoArticles
	}
	logger.Info("no new articles but quiz/book_review due — generating the private episode only (§7.1)",
		slog.Int("due_items", len(dueItems)), slog.Bool("book_review", brSel != nil))

	speakerName, err := p.resolveSpeakerName(ctx, opts.DryRun, logger)
	if err != nil {
		return err
	}

	corner := script.BuildQuizCorner(dueItems)
	introSeg := &entity.Segment{Position: 1, Kind: entity.SegmentKindIntro,
		Script: script.QuizOnlyIntro(p.Config.ShowName, now)}
	outroSeg := &entity.Segment{Kind: entity.SegmentKindOutro,
		Script: script.QuizOnlyOutro(p.Config.ShowName)}

	// クレジットは review 素材確定後に末尾へ付ける(§7.5)。base はここで組む。
	baseNotes := script.AppendQuizShowNotes(script.QuizOnlyShowNotesBase(), dueItems, p.Config.LearningURL)
	title, filename, err := p.episodeNaming(ctx, now, entity.FeedKindPrivate)
	if err != nil {
		return err
	}

	if opts.DryRun {
		// book_review / review は dry-run では生成しないので、プレビューの
		// セグメントは intro → quiz → outro のみ。book_review 対象は brSel、
		// 週次振り返りは素材(読み取りのみ)を印字する。
		reviewMat := p.previewWeeklyReview(ctx, logger, now)
		notes := baseNotes
		if reviewMat != nil {
			notes = script.AppendWeeklyReviewShowNotes(notes, *reviewMat)
		}
		notes = script.AppendVoicevoxCredit(notes, speakerName)
		segments := quizOnlySegments(introSeg, corner, nil, nil, outroSeg)
		return p.printDryRun(title, since, notes, segments, nil, dueItems, brSel, reviewMat)
	}

	tmpDir, err := os.MkdirTemp("", "radio-episode-")
	if err != nil {
		return fmt.Errorf("radio: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	var bookReview *bookReviewPlan
	if brSel != nil {
		bookReview = p.generateBookReview(ctx, logger, tmpDir, brSel)
	}
	if len(dueItems) == 0 && bookReview == nil {
		// クイズがなく book_review 生成も失敗 → 中身のない私的版は作らない。
		// カーソルは進めていない(生成失敗)ので翌日同じ箇所から (§7.1/§7.3)。
		// 週次振り返りだけのためにエピソードを起こすことはしない(D-1 スキップ
		// 契約を維持。手順7 の裁量判断: review は news/quiz/book が成立する日に
		// 相乗りする)。
		logger.Info("book_review generation failed on an articles-and-quiz-less day, skipping today (§7.1)")
		return ErrNoArticles
	}

	// §7.4 週次振り返り: エピソードが成立する日にのみ相乗り(上の skip 後)。
	// news なしのため book_review 尺ガードは無し(§7.1)— review は単純に加算。
	review := p.prepareWeeklyReview(ctx, logger, tmpDir, now)

	segments := quizOnlySegments(introSeg, corner, review, bookReview, outroSeg)

	segWavs, speechDuration, err := p.synthesize(ctx, tmpDir, []*entity.Segment{introSeg, outroSeg})
	if err != nil {
		return err
	}
	cornerWavs, cornerDuration, err := p.synthesizeQuizCorner(ctx, tmpDir, corner)
	if err != nil {
		return err
	}
	wavs := make([]string, 0, len(segWavs[0])+len(cornerWavs)+len(segWavs[1])+1)
	wavs = append(wavs, segWavs[0]...) // intro
	wavs = append(wavs, cornerWavs...) // quiz corner
	if review != nil {
		wavs = append(wavs, review.wavs...) // 週次振り返り
	}
	if bookReview != nil {
		wavs = append(wavs, bookReview.wavs...) // book_review
	}
	wavs = append(wavs, segWavs[1]...) // outro

	mp3Path := filepath.Join(tmpDir, filename)
	tags := tts.ID3{Title: title, Artist: p.Config.ShowName, Album: p.Config.ShowName,
		Date: now.Format("2006-01-02")}
	if err := p.Encoder.ConcatToMP3(ctx, wavs, mp3Path, tags); err != nil {
		return fmt.Errorf("radio: encode private episode: %w", err)
	}
	stat, err := os.Stat(mp3Path)
	if err != nil {
		return fmt.Errorf("radio: stat private mp3: %w", err)
	}
	audioPath, err := p.Transfer.Transfer(ctx, mp3Path, filename)
	if err != nil {
		return err
	}
	// クレジットは末尾。週次振り返りセクションは private 限定 (§10)。
	showNotes := baseNotes
	if review != nil {
		showNotes = script.AppendWeeklyReviewShowNotes(showNotes, review.material)
	}
	showNotes = script.AppendVoicevoxCredit(showNotes, speakerName)

	episode := &entity.Episode{
		FeedKind:    entity.FeedKindPrivate,
		Title:       title,
		ShowNotes:   showNotes,
		AudioPath:   audioPath,
		AudioBytes:  stat.Size(),
		DurationSec: int(math.Round((speechDuration + cornerDuration + weeklyReviewDuration(review) + bookReviewDuration(bookReview)).Seconds())),
		PublishedAt: now,
	}
	if err := p.Episodes.Create(ctx, episode, segments); err != nil {
		return fmt.Errorf("radio: insert private episode: %w", err)
	}
	// ジョブは積まない: notify_episode は §12-7 の契約(Run 内のコメント
	// 参照)により私的版では存在させない。regenerate_feed は no-op ハンドラ
	// (feed.xml はリクエスト毎に描画)なので省略する。
	p.recordAsked(ctx, logger, corner, episode.ID, now)
	if bookReview != nil {
		p.commitBookReviewProgress(ctx, logger, now, bookReview)
	}

	logger.Info("private-only episode generated (§7.1 記事ゼロ日)",
		slog.Int64("episode_id", episode.ID),
		slog.String("title", title),
		slog.String("audio_path", audioPath),
		slog.Int("duration_sec", episode.DurationSec),
		slog.Int("quiz_items", len(corner.Items)),
		slog.Bool("weekly_review", review != nil),
		slog.Bool("book_review", bookReview != nil))
	return nil
}

// quizOnlySegments assembles the no-articles private episode's segment rows:
// intro → quiz corner → review (§7.4, nil to omit) → book_review (§7.3, nil to
// omit) → outro, positions contiguous from 1. The outro's position is set here
// (it depends on how many quiz/review/book_review rows precede it).
func quizOnlySegments(intro *entity.Segment, corner script.QuizCorner, review *weeklyReviewPlan, bookReview *bookReviewPlan, outro *entity.Segment) []*entity.Segment {
	cornerSegs := corner.Segments(2) // intro=1, corner starts at 2
	segments := make([]*entity.Segment, 0, len(cornerSegs)+4)
	segments = append(segments, intro)
	segments = append(segments, cornerSegs...)
	pos := 2 + len(cornerSegs)
	if review != nil {
		segments = append(segments, &entity.Segment{
			Position: pos, Kind: entity.SegmentKindReview, Script: review.segment.Script})
		pos++
	}
	if bookReview != nil {
		segments = append(segments, &entity.Segment{
			Position: pos, Kind: entity.SegmentKindBookReview, Script: bookReview.segment.Script})
		pos++
	}
	outro.Position = pos
	segments = append(segments, outro)
	return segments
}

// privateEpisodeInput carries everything publishPrivateEpisode reuses from
// the public run — news wavs and segment scripts are shared, never
// re-synthesized (§7.1).
type privateEpisodeInput struct {
	tmpDir       string
	now          time.Time
	segments     []*entity.Segment // public segments: intro, news×N, outro
	segWavs      [][]string        // wav groups aligned with segments
	newsDuration time.Duration     // summed playing time of segWavs
	dueItems     []learning.Item
	corner       script.QuizCorner
	baseNotes    string // show notes before quiz section / credit
	speakerName  string
}

// publishPrivateEpisode assembles and ships the §7.1 private twin: intro →
// news×N (public wavs re-used) → quiz corner → outro. Best-effort by
// contract: every failure logs a warning and abandons the private episode
// only — the public one is already on the Pi and registered. With no due
// items the twin still ships (news-only): the admin's podcast app follows
// the private feed and expects a daily episode there.
//
// 手順6 への申し送り: the private episode's total length is available here
// as in.newsDuration + cornerDuration (before encoding) — the §7.1 18-minute
// guard should gate the book_review insertion on it.
func (p *Pipeline) publishPrivateEpisode(ctx context.Context, logger *slog.Logger, in privateEpisodeInput) {
	if p.Learning == nil {
		return // Phase 3 disabled: pre-Phase 3 behavior, public only
	}
	title, filename, err := p.episodeNaming(ctx, in.now, entity.FeedKindPrivate)
	if err != nil {
		logger.Warn("private episode skipped: naming failed (§9: 公開版は出荷済み)", slog.Any("error", err))
		return
	}
	cornerWavs, cornerDuration, err := p.synthesizeQuizCorner(ctx, in.tmpDir, in.corner)
	if err != nil {
		logger.Warn("private episode skipped: quiz corner TTS failed (§9)", slog.Any("error", err))
		return
	}

	// §7.4 週次振り返り: quiz の後・book_review の前に挿入(私的版のみ、土曜=
	// D-21)。テンプレート台本 (LLM 不使用) を先に TTS するので、その実測尺を
	// 下の book_review 尺ガードに加算できる(手順6 申し送り: review 分を算入)。
	review := p.prepareWeeklyReview(ctx, logger, in.tmpDir, in.now)

	// §7.3 book_review: selection → §7.1 尺ガード(news+quiz+review が18分に
	// 迫るなら翌日回し)→ Ollama 生成 + TTS。nil = 当日 book_review なし(private
	// は news+quiz(+review)、翌日カーソル位置から再開)。生成は review の後・
	// outro の前に挟む。
	bookReview := p.prepareBookReview(ctx, logger, in.tmpDir, in.now,
		in.newsDuration+cornerDuration+weeklyReviewDuration(review))

	// 私的版の並び: intro → news×N → quiz → review → book_review → outro。news の
	// wav は公開版と共用 (§7.1)。
	outroIdx := len(in.segWavs) - 1
	wavs := flattenWavs(in.segWavs[:outroIdx])
	wavs = append(wavs, cornerWavs...)
	if review != nil {
		wavs = append(wavs, review.wavs...)
	}
	if bookReview != nil {
		wavs = append(wavs, bookReview.wavs...)
	}
	wavs = append(wavs, in.segWavs[outroIdx]...)

	mp3Path := filepath.Join(in.tmpDir, filename)
	tags := tts.ID3{Title: title, Artist: p.Config.ShowName, Album: p.Config.ShowName,
		Date: in.now.Format("2006-01-02")}
	if err := p.Encoder.ConcatToMP3(ctx, wavs, mp3Path, tags); err != nil {
		logger.Warn("private episode skipped: encode failed (§9)", slog.Any("error", err))
		return
	}
	stat, err := os.Stat(mp3Path)
	if err != nil {
		logger.Warn("private episode skipped: stat failed (§9)", slog.Any("error", err))
		return
	}
	// A transfer that succeeds while a later step fails leaves an
	// unreferenced mp3 on the Pi; cleanup_old_media's orphan sweep collects
	// it after 48h (same window as a public rsync/INSERT failure).
	audioPath, err := p.Transfer.Transfer(ctx, mp3Path, filename)
	if err != nil {
		logger.Warn("private episode skipped: transfer failed (§9)", slog.Any("error", err))
		return
	}

	// U-13: the VOICEVOX credit is appended through the exact same helper as
	// the public path — クレジット無し配信のパスは feed_kind に依らず存在
	// しない (§7.5)。学習セクション(concept 一覧+採点リンク+週次振り返り)は
	// 私的版の show notes 限定 (§10)。クレジットは必ず末尾。
	showNotes := script.AppendQuizShowNotes(in.baseNotes, in.dueItems, p.Config.LearningURL)
	if review != nil {
		showNotes = script.AppendWeeklyReviewShowNotes(showNotes, review.material)
	}
	showNotes = script.AppendVoicevoxCredit(showNotes, in.speakerName)

	episode := &entity.Episode{
		FeedKind:    entity.FeedKindPrivate,
		Title:       title,
		ShowNotes:   showNotes,
		AudioPath:   audioPath,
		AudioBytes:  stat.Size(),
		DurationSec: int(math.Round((in.newsDuration + cornerDuration + weeklyReviewDuration(review) + bookReviewDuration(bookReview)).Seconds())),
		// The same selection timestamp as the public twin — deliberately:
		// the private feed folds the day's public/private pair by equal
		// published_at (feed.collapsePrivatePairs), and the public cursor
		// reads public rows only, so this can never move article selection.
		PublishedAt: in.now,
	}
	if err := p.Episodes.Create(ctx, episode, privateSegments(in.segments, in.corner, review, bookReview)); err != nil {
		logger.Warn("private episode skipped: insert failed (§9)", slog.Any("error", err))
		return
	}
	// ジョブは積まない: notify_episode は §12-7 の契約(Run 内のコメント
	// 参照)により私的版では存在させない。regenerate_feed は公開版の分が
	// 既に積まれている(かつ no-op ハンドラ)。
	p.recordAsked(ctx, logger, in.corner, episode.ID, in.now)
	// §7.3: カーソル前進・書籍クイズ INSERT は私的エピソード確定後に(生成
	// 失敗で先にカーソルだけ進む事故を防ぐ)。冪等(AdvanceCursor の guarded
	// WHERE + 同日 rev の HasBookReviewOn)。
	if bookReview != nil {
		p.commitBookReviewProgress(ctx, logger, in.now, bookReview)
	}

	logger.Info("private episode generated (§7.1 二本立て)",
		slog.Int64("episode_id", episode.ID),
		slog.String("title", title),
		slog.String("audio_path", audioPath),
		slog.Int("duration_sec", episode.DurationSec),
		slog.Int("quiz_items", len(in.corner.Items)),
		slog.Bool("weekly_review", review != nil),
		slog.Bool("book_review", bookReview != nil))
}

// bookReviewDuration is the plan's audio length, or zero when no book_review
// runs this episode.
func bookReviewDuration(plan *bookReviewPlan) time.Duration {
	if plan == nil {
		return 0
	}
	return plan.duration
}

// privateSegments builds the private episode's own segment rows: the public
// intro/news scripts copied verbatim (私的版は公開版の上位集合, §7.1), the
// quiz corner (1 項目 = 1 行, §7.2-4), the §7.4 週次振り返り (kind='review'),
// the §7.3 book_review (kind constant via entity, §12-6), then the outro —
// intro → news → quiz → review → book_review → outro (§7.4 の配置). Positions
// stay contiguous and unique. Fresh copies are mandatory — Create mutates
// ID/EpisodeID, and the passed-in rows are already committed under the public
// episode. review / bookReview may each be nil (segment omitted).
func privateSegments(public []*entity.Segment, corner script.QuizCorner, review *weeklyReviewPlan, bookReview *bookReviewPlan) []*entity.Segment {
	cornerSegs := corner.Segments(len(public)) // outro の位置から採番
	out := make([]*entity.Segment, 0, len(public)+len(cornerSegs)+2)
	copySegment := func(s *entity.Segment, position int) *entity.Segment {
		return &entity.Segment{Position: position, Kind: s.Kind, ArticleID: s.ArticleID, Script: s.Script}
	}
	for _, s := range public[:len(public)-1] {
		out = append(out, copySegment(s, s.Position))
	}
	out = append(out, cornerSegs...)
	pos := len(public) + len(cornerSegs)
	// (手順7) 週次振り返りは quiz の後・book_review の前 (§7.4)。
	if review != nil {
		out = append(out, &entity.Segment{
			Position: pos, Kind: entity.SegmentKindReview, Script: review.segment.Script})
		pos++
	}
	// (手順6) book_review セグメントは review の後・outro の前に挿入。
	if bookReview != nil {
		out = append(out, &entity.Segment{
			Position: pos, Kind: entity.SegmentKindBookReview, Script: bookReview.segment.Script})
		pos++
	}
	out = append(out, copySegment(public[len(public)-1], pos))
	return out
}

// synthesizeQuizCorner renders the §7.2 corner as wav files: the lead, then
// per item question → 3秒無音 → answer. The silence wav is fabricated once
// per run from the exact format of this run's first synthesized corner wav
// (§12-5: VOICEVOX 出力とサンプリングレート・チャンネル数・ビット深度が
// 一致しない無音は concat を壊す — deriving it from a real output makes the
// match structural) and its single file is referenced repeatedly from the
// concat list. An empty corner yields no wavs and no error.
func (p *Pipeline) synthesizeQuizCorner(ctx context.Context, dir string, corner script.QuizCorner) ([]string, time.Duration, error) {
	if len(corner.Items) == 0 {
		return nil, 0, nil
	}
	var wavs []string
	var total time.Duration
	write := func(prefix string, audios []tts.Audio) error {
		for j, audio := range audios {
			path := filepath.Join(dir, fmt.Sprintf("%s_%03d.wav", prefix, j))
			if err := os.WriteFile(path, audio.Data, 0o600); err != nil {
				return fmt.Errorf("radio: write quiz wav: %w", err)
			}
			wavs = append(wavs, path)
			total += audio.Duration
		}
		return nil
	}

	leadAudios, err := p.TTS.SynthesizeScript(ctx, corner.Lead)
	if err != nil {
		return nil, 0, fmt.Errorf("radio: tts quiz lead: %w", err)
	}
	format, err := tts.ParseWavFormat(leadAudios[0].Data)
	if err != nil {
		return nil, 0, fmt.Errorf("radio: read VOICEVOX wav format for silence: %w", err)
	}
	silence, err := tts.SilenceWav(format, quizPause)
	if err != nil {
		return nil, 0, fmt.Errorf("radio: build silence wav: %w", err)
	}
	silencePath := filepath.Join(dir, "quiz_silence.wav")
	if err := os.WriteFile(silencePath, silence, 0o600); err != nil {
		return nil, 0, fmt.Errorf("radio: write silence wav: %w", err)
	}
	if err := write("quiz_lead", leadAudios); err != nil {
		return nil, 0, err
	}

	for i, item := range corner.Items {
		questionAudios, err := p.TTS.SynthesizeScript(ctx, item.Question)
		if err != nil {
			return nil, 0, fmt.Errorf("radio: tts quiz question %d: %w", i+1, err)
		}
		if err := write(fmt.Sprintf("quiz_%03d_q", i), questionAudios); err != nil {
			return nil, 0, err
		}
		wavs = append(wavs, silencePath)
		total += quizPause
		answerAudios, err := p.TTS.SynthesizeScript(ctx, item.Answer)
		if err != nil {
			return nil, 0, fmt.Errorf("radio: tts quiz answer %d: %w", i+1, err)
		}
		if err := write(fmt.Sprintf("quiz_%03d_a", i), answerAudios); err != nil {
			return nil, 0, err
		}
	}
	return wavs, total, nil
}

// autoResolve applies the D-17 auto-advance (result='auto' after 48h) at
// the §3-mandated moment: right before the day's selection. Failures only
// warn (§9): ListDue excludes items with stale ungraded logs, so a skipped
// resolution can never double-ask — the items simply wait for tomorrow.
// Dry-run skips it entirely (a dry-run makes no DB writes), accepting that
// the printed selection may differ slightly from the real run's.
func (p *Pipeline) autoResolve(ctx context.Context, now time.Time, dryRun bool, logger *slog.Logger) {
	if p.Learning == nil {
		return
	}
	if dryRun {
		logger.Info("dry-run: auto-resolve skipped (DB write)")
		return
	}
	if p.LearningCfg.AutoResolveAfter <= 0 || len(p.LearningCfg.Ladder) == 0 {
		// Unreachable via LoadConfig; guards direct construction, where a
		// zero AutoResolveAfter would silently resolve TODAY's fresh logs.
		logger.Warn("learning config incomplete, skipping auto-resolve")
		return
	}
	cutoffDay := learning.BroadcastDay(now.Add(-p.LearningCfg.AutoResolveAfter))
	resolveDay := learning.BroadcastDay(now)
	resolved, err := p.Learning.AutoResolve(ctx, cutoffDay, resolveDay, p.LearningCfg.Ladder)
	if err != nil {
		logger.Warn("auto-resolve failed, continuing — asking goes on (§9)", slog.Any("error", err))
		return
	}
	if resolved > 0 {
		logger.Info("ungraded review logs auto-resolved (D-17)",
			slog.Int("resolved", resolved),
			slog.String("cutoff_day", learning.FormatDay(cutoffDay)))
	}
}

// listDueItems selects the day's quiz candidates (§6.3: due_on ASC, id ASC,
// 最大 S 件). Read-only. A failure degrades to an empty selection with a
// warning: the private episode then ships news-only, and the items — whose
// state never changes on asking (§12-2) — are naturally retried tomorrow.
func (p *Pipeline) listDueItems(ctx context.Context, now time.Time, logger *slog.Logger) []learning.Item {
	if p.Learning == nil || p.LearningCfg.Slots <= 0 {
		return nil
	}
	items, err := p.Learning.ListDue(ctx, learning.BroadcastDay(now), p.LearningCfg.Slots)
	if err != nil {
		logger.Warn("quiz selection failed, private episode degrades to news-only (§9)",
			slog.Any("error", err))
		return nil
	}
	return items
}

// recordAsked writes the day's review logs (§6.3: 選定後 INSERT、result
// NULL)— episode_id is the PRIVATE episode that actually carried the
// corner. ON CONFLICT (item_id, asked_on) DO NOTHING makes a same-day rev
// re-run keep the first rev's rows (§12-2). Best-effort: the episode is
// already shipped, and an unrecorded ask self-heals — the item stays due
// and is re-asked tomorrow, which D-17 tolerates by design.
func (p *Pipeline) recordAsked(ctx context.Context, logger *slog.Logger, corner script.QuizCorner, episodeID int64, now time.Time) {
	if p.Learning == nil || len(corner.Items) == 0 {
		return
	}
	askedOn := learning.BroadcastDay(now)
	if err := p.Learning.RecordAsked(ctx, corner.ItemIDs(), episodeID, askedOn); err != nil {
		logger.Warn("failed to record asked review logs — items stay due and re-ask tomorrow (§9)",
			slog.Int64("episode_id", episodeID), slog.Any("error", err))
		return
	}
	logger.Info("review logs recorded (§6.3)",
		slog.Int("items", len(corner.Items)),
		slog.Int64("episode_id", episodeID),
		slog.String("asked_on", learning.FormatDay(askedOn)))
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

// resolveSpeakerName resolves the U-13 credit speaker. A resolution failure
// aborts a real run — shipping an episode without its 「VOICEVOX:話者名」
// credit would violate the VOICEVOX terms of use. Dry-run keeps going with
// a placeholder: it never distributes audio and must stay usable on
// machines without the engine (D-2).
func (p *Pipeline) resolveSpeakerName(ctx context.Context, dryRun bool, logger *slog.Logger) (string, error) {
	speakerName, err := p.TTS.SpeakerName(ctx)
	if err != nil {
		if !dryRun {
			return "", fmt.Errorf("radio: resolve VOICEVOX speaker name for credit (U-13): %w", err)
		}
		logger.Warn("VOICEVOX speaker name unresolved, dry-run uses a placeholder", slog.Any("error", err))
		speakerName = "(話者名未解決)"
	}
	return speakerName, nil
}

// selectionCursor returns the article-selection cursor: an explicit
// override, or the previous public episode's published_at, or the zero time
// on the very first run (§6-1: 前回 public エピソード以降). Private episodes
// never move the cursor.
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
// day becomes "…revN" with a distinct filename (§6-6). Rev counting is per
// feed kind, and private filenames take a "-private" suffix (§7.1): the two
// name families can never collide — not even when one kind's rev count ran
// ahead of the other's (e.g. 私的版だけ落ちた日の再実行).
func (p *Pipeline) episodeNaming(ctx context.Context, now time.Time, feedKind string) (title, filename string, err error) {
	day := now.Format("2006-01-02")
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	count, err := p.Episodes.CountByKindSince(ctx, feedKind, startOfDay)
	if err != nil {
		return "", "", fmt.Errorf("radio: count today's %s episodes: %w", feedKind, err)
	}
	title = fmt.Sprintf("%s %s", p.Config.ShowName, day)
	base := day
	if count > 0 {
		rev := count + 1
		title = fmt.Sprintf("%s rev%d", title, rev)
		base = fmt.Sprintf("%s-rev%d", day, rev)
	}
	if feedKind == entity.FeedKindPrivate {
		base += "-private"
	}
	return title, base + ".mp3", nil
}

// synthesize renders every segment through TTS into wav files inside dir,
// returning the wav paths grouped per segment (concat リストを公開/私的の
// 2通り組み立てるため) and the summed playing time.
func (p *Pipeline) synthesize(ctx context.Context, dir string, segments []*entity.Segment) ([][]string, time.Duration, error) {
	groups := make([][]string, 0, len(segments))
	var total time.Duration
	for i, segment := range segments {
		audios, err := p.TTS.SynthesizeScript(ctx, segment.Script)
		if err != nil {
			return nil, 0, fmt.Errorf("radio: tts segment %d (%s): %w", i+1, segment.Kind, err)
		}
		group := make([]string, 0, len(audios))
		for j, audio := range audios {
			path := filepath.Join(dir, fmt.Sprintf("seg_%03d_%03d.wav", i, j))
			if err := os.WriteFile(path, audio.Data, 0o600); err != nil {
				return nil, 0, fmt.Errorf("radio: write wav: %w", err)
			}
			group = append(group, path)
			total += audio.Duration
		}
		groups = append(groups, group)
	}
	return groups, total, nil
}

// flattenWavs joins per-segment wav groups into one concat-list order.
func flattenWavs(groups [][]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

// printDryRun writes the would-be episode to Out (D-2: 台本を目視で調整).
// The report is the sole product of a dry-run, so it is rendered in memory
// and written once, with the write error surfaced to the caller. Learning
// item drafts and the quiz selection are printed for inspection but nothing
// is written — no InsertItem, no AutoResolve, no RecordAsked (dry-run makes
// no DB writes).
func (p *Pipeline) printDryRun(title string, since time.Time, showNotes string, segments []*entity.Segment, drafts []script.QuizDraft, dueItems []learning.Item, bookReview *bookReviewSelection, weekly *learning.WeeklyReview) error {
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
	if len(dueItems) > 0 {
		fmt.Fprintf(&sb, "--- quiz selection: %d item(s) due (dry-run: AutoResolve/RecordAsked skipped) ---\n", len(dueItems))
		for i, item := range dueItems {
			fmt.Fprintf(&sb, "%d. item %d [stage %d, due %s] %s\n",
				i+1, item.ID, item.Stage, learning.FormatDay(item.DueOn), item.Concept)
		}
		sb.WriteString("\n")
	}
	// §7.3 book_review 対象(dry-run: Ollama 生成・カーソル前進・クイズ INSERT
	// は行わない)。尺ガード(§7.1)は実 run の音声尺に依存するため dry-run では
	// 評価せず、対象書名とチャンク範囲のみ印字する。
	if bookReview != nil {
		last := bookReview.chunks[len(bookReview.chunks)-1].Position
		fmt.Fprintf(&sb, "--- book_review target (dry-run: 生成/カーソル前進/クイズ INSERT なし) ---\n書名: %s (book %d)\nチャンク範囲: position %d..%d (%d 個), cursor %d -> %d%s\n\n",
			bookReview.book.Title, bookReview.book.ID,
			bookReview.chunks[0].Position, last, len(bookReview.chunks),
			bookReview.book.Cursor, bookReview.newCursor, finishedNote(bookReview.finished))
	}
	// §7.4 週次振り返り(dry-run: TTS なし)。素材のみ印字する。曜日でない/
	// 素材ゼロの週は weekly==nil でここには出ない。
	if weekly != nil {
		if body, ok := script.BuildWeeklyReview(*weekly); ok {
			fmt.Fprintf(&sb, "--- weekly review (dry-run: 週次振り返り、卒業 %d件, 再紹介 %q) ---\n%s\n\n",
				weekly.GraduatedCount, weekly.Reintroduced, body)
		}
	}
	if _, err := io.WriteString(out, sb.String()); err != nil {
		return fmt.Errorf("radio: write dry-run report: %w", err)
	}
	return nil
}

// finishedNote annotates the dry-run cursor line when this batch would finish
// the book (§7.3: 末尾到達で finished).
func finishedNote(finished bool) string {
	if finished {
		return " (末尾到達 → finished)"
	}
	return ""
}
