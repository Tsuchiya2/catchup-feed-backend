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
	"time"

	"catchup-feed/internal/domain/entity"
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
// by script.Generator.
type ScriptGenerator interface {
	GenerateEpisode(ctx context.Context, date time.Time, articles []repository.RadioArticle) ([]*entity.Segment, error)
}

// Synthesizer renders one segment script as sentence WAVs (§6-3).
// Satisfied by tts.Voicevox.
type Synthesizer interface {
	SynthesizeScript(ctx context.Context, script string) ([]tts.Audio, error)
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
	Config   Config
	Logger   *slog.Logger
	Now      func() time.Time // nil = time.Now
	Out      io.Writer        // dry-run output; nil = os.Stdout
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

	// --- §6-2 台本生成 ---
	segments, err := p.Script.GenerateEpisode(ctx, now, featured)
	if err != nil {
		return fmt.Errorf("radio: generate script: %w", err)
	}
	showNotes := script.BuildShowNotes(featured, overflow)

	// --- §6-6 冪等性: 同日再実行は rev 付き新規版 ---
	title, filename, err := p.episodeNaming(ctx, now)
	if err != nil {
		return err
	}

	if opts.DryRun {
		p.printDryRun(title, since, showNotes, segments)
		return nil
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

	logger.Info("episode generated",
		slog.Int64("episode_id", episode.ID),
		slog.String("title", title),
		slog.String("audio_path", audioPath),
		slog.Int64("audio_bytes", episode.AudioBytes),
		slog.Int("duration_sec", episode.DurationSec),
		slog.Int("segments", len(segments)))
	return nil
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
func (p *Pipeline) printDryRun(title string, since time.Time, showNotes string, segments []*entity.Segment) {
	out := p.Out
	if out == nil {
		out = os.Stdout
	}
	fmt.Fprintf(out, "=== dry-run: %s ===\n", title)
	fmt.Fprintf(out, "selection since: %s\n\n", since.Format(time.RFC3339))
	fmt.Fprintf(out, "--- show notes ---\n%s\n\n", showNotes)
	for _, segment := range segments {
		fmt.Fprintf(out, "--- segment %d [%s] ---\n%s\n\n", segment.Position, segment.Kind, segment.Script)
	}
}
