package radio_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/radio"
	"catchup-feed/internal/repository"
	"catchup-feed/internal/tts"
)

// ---- fakes ----

type fakeArticles struct {
	articles  []repository.RadioArticle
	err       error
	lastSince time.Time
	lastLimit int
}

func (f *fakeArticles) ListSummarizedSince(_ context.Context, since time.Time, limit int) ([]repository.RadioArticle, error) {
	f.lastSince = since
	f.lastLimit = limit
	return f.articles, f.err
}

type fakeEpisodes struct {
	previous    []*entity.Episode
	todayCount  int
	created     *entity.Episode
	createdSegs []*entity.Segment
	createErr   error
	nextID      int64
}

func (f *fakeEpisodes) ListByKind(_ context.Context, feedKind string, _ int) ([]*entity.Episode, error) {
	var out []*entity.Episode
	for _, ep := range f.previous {
		if ep.FeedKind == feedKind {
			out = append(out, ep)
		}
	}
	return out, nil
}

func (f *fakeEpisodes) CountByKindSince(_ context.Context, _ string, _ time.Time) (int, error) {
	return f.todayCount, nil
}

func (f *fakeEpisodes) Create(_ context.Context, episode *entity.Episode, segments []*entity.Segment) error {
	if f.createErr != nil {
		return f.createErr
	}
	if f.nextID == 0 {
		f.nextID = 1
	}
	episode.ID = f.nextID
	f.created = episode
	f.createdSegs = segments
	return nil
}

type enqueued struct {
	kind    string
	payload json.RawMessage
}

type fakeJobs struct {
	jobs []enqueued
	err  error
}

func (f *fakeJobs) Enqueue(_ context.Context, kind string, payload json.RawMessage, _ time.Time) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.jobs = append(f.jobs, enqueued{kind: kind, payload: payload})
	return int64(len(f.jobs)), nil
}

type fakeScript struct {
	err      error
	articles []repository.RadioArticle // captured input (C-12 flow check)
}

func (f *fakeScript) GenerateEpisode(_ context.Context, _ time.Time, articles []repository.RadioArticle) ([]*entity.Segment, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.articles = articles
	segments := []*entity.Segment{{Position: 1, Kind: entity.SegmentKindIntro, Script: "イントロ。"}}
	for i, a := range articles {
		id := a.ID
		segments = append(segments, &entity.Segment{
			Position: i + 2, Kind: entity.SegmentKindNews, ArticleID: &id,
			Script: fmt.Sprintf("ニュース%d。", i+1),
		})
	}
	segments = append(segments, &entity.Segment{
		Position: len(articles) + 2, Kind: entity.SegmentKindOutro, Script: "アウトロ。",
	})
	return segments, nil
}

type fakeTTS struct {
	err            error
	calls          int
	speakerName    string // "" = "ずんだもん"
	speakerNameErr error
}

func (f *fakeTTS) SynthesizeScript(_ context.Context, _ string) ([]tts.Audio, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return []tts.Audio{{Data: []byte("wav"), Duration: 30 * time.Second}}, nil
}

func (f *fakeTTS) SpeakerName(_ context.Context) (string, error) {
	if f.speakerNameErr != nil {
		return "", f.speakerNameErr
	}
	if f.speakerName == "" {
		return "ずんだもん", nil
	}
	return f.speakerName, nil
}

type fakeEncoder struct {
	err      error
	outPath  string
	wavCount int
	tags     tts.ID3
}

func (f *fakeEncoder) ConcatToMP3(_ context.Context, wavPaths []string, outPath string, tags tts.ID3) error {
	if f.err != nil {
		return f.err
	}
	f.outPath = outPath
	f.wavCount = len(wavPaths)
	f.tags = tags
	return os.WriteFile(outPath, []byte("mp3-bytes"), 0o600)
}

type fakeTransfer struct {
	err       error
	audioPath string
	localPath string
}

func (f *fakeTransfer) Transfer(_ context.Context, localPath, filename string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.localPath = localPath
	f.audioPath = "/data/episodes/" + filename
	return f.audioPath, nil
}

// ---- harness ----

type deps struct {
	articles *fakeArticles
	episodes *fakeEpisodes
	jobs     *fakeJobs
	script   *fakeScript
	tts      *fakeTTS
	encoder  *fakeEncoder
	transfer *fakeTransfer
	out      *bytes.Buffer
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 5, 4, 30, 0, 0, time.UTC)
}

func newPipeline(t *testing.T, d *deps) *radio.Pipeline {
	t.Helper()
	cfg := radio.Config{
		ShowName:    "pulse",
		MaxArticles: 8,
		Location:    time.UTC,
		EpisodesDir: "/data/episodes",
	}
	return &radio.Pipeline{
		Articles: d.articles,
		Episodes: d.episodes,
		Jobs:     d.jobs,
		Script:   d.script,
		TTS:      d.tts,
		Encoder:  d.encoder,
		Transfer: d.transfer,
		Config:   cfg,
		Now:      fixedNow,
		Out:      d.out,
	}
}

func defaultDeps() *deps {
	return &deps{
		articles: &fakeArticles{articles: []repository.RadioArticle{
			{ID: 10, Title: "記事A", URL: "https://example.com/a", Category: "golang",
				SourceName: "Go Blog", Summary: "要約A", PublishedAt: fixedNow().Add(-10 * time.Hour)},
			{ID: 20, Title: "記事B", URL: "https://example.com/b", Category: "ai",
				SourceName: "AI News", Summary: "要約B", PublishedAt: fixedNow().Add(-5 * time.Hour)},
		}},
		episodes: &fakeEpisodes{nextID: 55},
		jobs:     &fakeJobs{},
		script:   &fakeScript{},
		tts:      &fakeTTS{},
		encoder:  &fakeEncoder{},
		transfer: &fakeTransfer{},
		out:      &bytes.Buffer{},
	}
}

// ---- tests ----

func TestPipeline_Run_Success(t *testing.T) {
	d := defaultDeps()
	p := newPipeline(t, d)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	// episode registration (§6-5)
	ep := d.episodes.created
	require.NotNil(t, ep)
	assert.Equal(t, entity.FeedKindPublic, ep.FeedKind)
	assert.Equal(t, "pulse 2026-07-05", ep.Title)
	assert.True(t, ep.PublishedAt.Equal(fixedNow()),
		"published_at must be the selection timestamp, not left to the DB INSERT time")
	assert.Equal(t, "/data/episodes/2026-07-05.mp3", ep.AudioPath)
	assert.Equal(t, int64(len("mp3-bytes")), ep.AudioBytes)
	assert.Equal(t, 120, ep.DurationSec, "4 segments x 30s")
	assert.Contains(t, ep.ShowNotes, "https://example.com/a")
	assert.Contains(t, ep.ShowNotes, "https://example.com/b")
	assert.True(t, strings.HasSuffix(ep.ShowNotes, "音声合成: VOICEVOX:ずんだもん"),
		"U-13: show notes must end with the VOICEVOX speaker credit, got:\n%s", ep.ShowNotes)
	require.Len(t, d.episodes.createdSegs, 4)

	// jobs for the Pi worker (§6-5, C-4)
	require.Len(t, d.jobs.jobs, 2)
	assert.Equal(t, entity.JobKindRegenerateFeed, d.jobs.jobs[0].kind)
	assert.Equal(t, entity.JobKindNotifyEpisode, d.jobs.jobs[1].kind)
	assert.JSONEq(t, `{"episode_id":55}`, string(d.jobs.jobs[1].payload))

	// encoding got every wav and the ID3 tags (§6-4)
	assert.Equal(t, 4, d.encoder.wavCount)
	assert.Equal(t, "pulse 2026-07-05", d.encoder.tags.Title)
	assert.Equal(t, "2026-07-05", d.encoder.tags.Date)
}

// TestPipeline_Run_WindowClosure pins the SELECT-to-INSERT window fix as a
// two-run scenario: run 1 selects at T0 and records T0 as published_at, so
// run 2's cursor is exactly T0. A summary the worker inserted while run 1
// was still generating (created_at = T0+ε) satisfies the repository query
// `created_at > T0` and is selected by run 2 — nothing is lost, no matter
// how long run 1's LLM/TTS stages took (Ollama 縮退モードで顕著な窓).
func TestPipeline_Run_WindowClosure(t *testing.T) {
	t0 := fixedNow()

	// Run 1: batch selects at t0, INSERT happens arbitrarily later.
	run1 := defaultDeps()
	require.NoError(t, newPipeline(t, run1).Run(context.Background(), radio.RunOptions{}))
	require.NotNil(t, run1.episodes.created)
	require.True(t, run1.episodes.created.PublishedAt.Equal(t0),
		"run 1 must record its selection timestamp as published_at")

	// Run 2: the previous public episode is run 1's row.
	run2 := defaultDeps()
	run2.episodes.previous = []*entity.Episode{run1.episodes.created}
	p2 := newPipeline(t, run2)
	p2.Now = func() time.Time { return t0.Add(24 * time.Hour) }

	require.NoError(t, p2.Run(context.Background(), radio.RunOptions{}))

	assert.True(t, run2.articles.lastSince.Equal(t0),
		"run 2's cursor must equal run 1's selection time, not its INSERT time")
}

func TestPipeline_Run_SelectionCursor(t *testing.T) {
	t.Run("first run uses the zero time", func(t *testing.T) {
		d := defaultDeps()
		require.NoError(t, newPipeline(t, d).Run(context.Background(), radio.RunOptions{}))
		assert.True(t, d.articles.lastSince.IsZero())
	})

	t.Run("subsequent runs start from the last public episode", func(t *testing.T) {
		d := defaultDeps()
		prev := fixedNow().Add(-24 * time.Hour)
		d.episodes.previous = []*entity.Episode{
			{ID: 1, FeedKind: entity.FeedKindPublic, PublishedAt: prev},
		}
		require.NoError(t, newPipeline(t, d).Run(context.Background(), radio.RunOptions{}))
		assert.Equal(t, prev, d.articles.lastSince, "§6-1: 前回 public エピソード以降")
	})

	t.Run("private episodes never move the cursor", func(t *testing.T) {
		d := defaultDeps()
		d.episodes.previous = []*entity.Episode{
			{ID: 2, FeedKind: entity.FeedKindPrivate, PublishedAt: fixedNow().Add(-time.Hour)},
		}
		require.NoError(t, newPipeline(t, d).Run(context.Background(), radio.RunOptions{}))
		assert.True(t, d.articles.lastSince.IsZero())
	})

	t.Run("explicit override wins", func(t *testing.T) {
		d := defaultDeps()
		since := fixedNow().Add(-48 * time.Hour)
		require.NoError(t, newPipeline(t, d).Run(context.Background(), radio.RunOptions{Since: &since}))
		assert.Equal(t, since, d.articles.lastSince)
	})
}

func TestPipeline_Run_NoArticlesSkips(t *testing.T) {
	d := defaultDeps()
	d.articles.articles = nil
	p := newPipeline(t, d)

	err := p.Run(context.Background(), radio.RunOptions{})

	require.ErrorIs(t, err, radio.ErrNoArticles, "D-1: 記事ゼロはスキップ")
	assert.Nil(t, d.episodes.created)
	assert.Empty(t, d.jobs.jobs)
	assert.Equal(t, 0, d.tts.calls)
}

func TestPipeline_Run_RevOnSameDayRerun(t *testing.T) {
	tests := []struct {
		name       string
		todayCount int
		wantTitle  string
		wantAudio  string
	}{
		{name: "first run of the day", todayCount: 0,
			wantTitle: "pulse 2026-07-05", wantAudio: "/data/episodes/2026-07-05.mp3"},
		{name: "second run appends rev2", todayCount: 1,
			wantTitle: "pulse 2026-07-05 rev2", wantAudio: "/data/episodes/2026-07-05-rev2.mp3"},
		{name: "third run appends rev3", todayCount: 2,
			wantTitle: "pulse 2026-07-05 rev3", wantAudio: "/data/episodes/2026-07-05-rev3.mp3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := defaultDeps()
			d.episodes.todayCount = tt.todayCount
			require.NoError(t, newPipeline(t, d).Run(context.Background(), radio.RunOptions{}))

			require.NotNil(t, d.episodes.created)
			assert.Equal(t, tt.wantTitle, d.episodes.created.Title, "§6-6: rev 付き新規版")
			assert.Equal(t, tt.wantAudio, d.episodes.created.AudioPath,
				"same-day re-run must not overwrite the previous mp3")
		})
	}
}

// TestPipeline_Run_FailuresWriteNothing pins §6-6: any mid-pipeline failure
// leaves the DB untouched (no episode row, no jobs).
func TestPipeline_Run_FailuresWriteNothing(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*deps)
		wantSub string
	}{
		{
			name:    "article selection fails",
			mutate:  func(d *deps) { d.articles.err = errors.New("pg down") },
			wantSub: "select articles",
		},
		{
			name:    "script generation fails (all LLM providers down)",
			mutate:  func(d *deps) { d.script.err = errors.New("all generate providers failed") },
			wantSub: "generate script",
		},
		{
			name:    "VOICEVOX unreachable skips the day (§8)",
			mutate:  func(d *deps) { d.tts.err = errors.New("connection refused") },
			wantSub: "tts segment",
		},
		{
			// U-13: クレジット表記なしでの配信は不可 — the run aborts
			// instead of shipping a credit-less episode.
			name:    "VOICEVOX speaker name unresolved skips the day (U-13)",
			mutate:  func(d *deps) { d.tts.speakerNameErr = errors.New("connection refused") },
			wantSub: "speaker name",
		},
		{
			name:    "ffmpeg failure",
			mutate:  func(d *deps) { d.encoder.err = errors.New("exit status 1") },
			wantSub: "encode",
		},
		{
			name:    "rsync failure",
			mutate:  func(d *deps) { d.transfer.err = errors.New("rsync: connection unexpectedly closed") },
			wantSub: "rsync",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := defaultDeps()
			tt.mutate(d)
			err := newPipeline(t, d).Run(context.Background(), radio.RunOptions{})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
			assert.Nil(t, d.episodes.created, "failed run must not insert an episode (§6-6)")
			assert.Empty(t, d.jobs.jobs, "failed run must not enqueue jobs")
		})
	}
}

func TestPipeline_Run_EpisodeInsertFailureEnqueuesNothing(t *testing.T) {
	d := defaultDeps()
	d.episodes.createErr = errors.New("unique violation")

	err := newPipeline(t, d).Run(context.Background(), radio.RunOptions{})

	require.Error(t, err)
	assert.Empty(t, d.jobs.jobs, "jobs are enqueued only after the episode row exists")
}

func TestPipeline_Run_DryRun(t *testing.T) {
	d := defaultDeps()
	p := newPipeline(t, d)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{DryRun: true}))

	assert.Equal(t, 0, d.tts.calls, "dry-run stops before TTS")
	assert.Nil(t, d.episodes.created, "dry-run writes nothing to the DB")
	assert.Empty(t, d.jobs.jobs)

	printed := d.out.String()
	assert.Contains(t, printed, "pulse 2026-07-05")
	assert.Contains(t, printed, "イントロ。", "scripts go to stdout for prompt/speaker tuning (D-2)")
	assert.Contains(t, printed, "https://example.com/a", "show notes preview")
	assert.Contains(t, printed, "音声合成: VOICEVOX:ずんだもん", "show notes preview carries the credit (U-13)")
}

// TestPipeline_Run_DryRunWithoutEngine pins that dry-run stays usable when
// the VOICEVOX engine is unreachable (D-2: プロンプト調整はエンジン不要):
// the unresolved speaker name becomes a placeholder instead of an error.
func TestPipeline_Run_DryRunWithoutEngine(t *testing.T) {
	d := defaultDeps()
	d.tts.speakerNameErr = errors.New("connection refused")
	p := newPipeline(t, d)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{DryRun: true}))

	assert.Nil(t, d.episodes.created)
	assert.Contains(t, d.out.String(), "音声合成: VOICEVOX:(話者名未解決)")
}

func TestPipeline_Run_OverflowGoesToShowNotesOnly(t *testing.T) {
	d := defaultDeps()
	// 3 articles, cap 2: the oldest drops to the show notes.
	d.articles.articles = append(d.articles.articles, repository.RadioArticle{
		ID: 30, Title: "記事C", URL: "https://example.com/c", Category: "infra",
		SourceName: "Infra Weekly", Summary: "要約C", PublishedAt: fixedNow().Add(-2 * time.Hour),
	})
	p := newPipeline(t, d)
	p.Config.MaxArticles = 2

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	require.Len(t, d.script.articles, 2, "only featured articles reach the script LLM")
	assert.Contains(t, d.episodes.created.ShowNotes, "https://example.com/a",
		"overflow article still appears in the show notes (§6-1)")
	assert.Contains(t, d.episodes.created.ShowNotes, "紹介しきれなかった記事")
}
