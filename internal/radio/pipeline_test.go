package radio_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/learning"
	"catchup-feed/internal/radio"
	"catchup-feed/internal/repository"
	"catchup-feed/internal/script"
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
	todayCounts map[string]int // per feed kind (rev numbering)
	countErr    error
	created     []*entity.Episode
	createdSegs [][]*entity.Segment
	createErr   error // fails every Create
	// privateCreateErr fails only feed_kind='private' inserts (縮退テスト用).
	privateCreateErr error
	nextID           int64
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

func (f *fakeEpisodes) CountByKindSince(_ context.Context, feedKind string, _ time.Time) (int, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.todayCounts[feedKind], nil
}

func (f *fakeEpisodes) Create(_ context.Context, episode *entity.Episode, segments []*entity.Segment) error {
	if f.createErr != nil {
		return f.createErr
	}
	if episode.FeedKind == entity.FeedKindPrivate && f.privateCreateErr != nil {
		return f.privateCreateErr
	}
	if f.nextID == 0 {
		f.nextID = 1
	}
	episode.ID = f.nextID
	f.nextID++
	f.created = append(f.created, episode)
	f.createdSegs = append(f.createdSegs, segments)
	return nil
}

// byKind returns the last created episode of the kind plus its segments.
func (f *fakeEpisodes) byKind(kind string) (*entity.Episode, []*entity.Segment) {
	for i := len(f.created) - 1; i >= 0; i-- {
		if f.created[i].FeedKind == kind {
			return f.created[i], f.createdSegs[i]
		}
	}
	return nil, nil
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
	err       error
	called    bool
	articles  []repository.RadioArticle // captured input (C-12 flow check)
	quizCount int                       // captured input (Phase 3 §5.1/§5.2)
	drafts    []script.QuizDraft        // returned when quizCount > 0
}

func (f *fakeScript) GenerateEpisode(_ context.Context, _ time.Time, articles []repository.RadioArticle, quizCount int) ([]*entity.Segment, []script.QuizDraft, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	f.called = true
	f.articles = articles
	f.quizCount = quizCount
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
	var drafts []script.QuizDraft
	if quizCount > 0 {
		drafts = f.drafts
	}
	return segments, drafts, nil
}

// autoResolveCall captures one AutoResolve invocation.
type autoResolveCall struct {
	cutoffDay  time.Time
	resolveDay time.Time
	ladder     []int
}

// recordAskedCall captures one RecordAsked invocation.
type recordAskedCall struct {
	itemIDs   []int64
	episodeID int64
	askedOn   time.Time
}

// fakeLearning implements radio.LearningStore and records every call.
type fakeLearning struct {
	overdue     int
	overdueErr  error
	hasToday    bool
	hasTodayErr error
	insertErr   error

	due            []learning.Item
	dueErr         error
	recordAskedErr error
	autoResolveErr error
	autoResolveN   int

	countDays    []time.Time
	hasDays      []time.Time
	inserted     []learning.NewItem
	dueOns       []time.Time
	listDueDays  []time.Time
	listDueLimit int
	asked        []recordAskedCall
	autoResolves []autoResolveCall
}

func (f *fakeLearning) CountOverdueActive(_ context.Context, day time.Time) (int, error) {
	f.countDays = append(f.countDays, day)
	return f.overdue, f.overdueErr
}

func (f *fakeLearning) HasArticleItemCreatedOn(_ context.Context, day time.Time) (bool, error) {
	f.hasDays = append(f.hasDays, day)
	return f.hasToday, f.hasTodayErr
}

func (f *fakeLearning) InsertItem(_ context.Context, item learning.NewItem, dueOn time.Time) (int64, error) {
	if f.insertErr != nil {
		return 0, f.insertErr
	}
	f.inserted = append(f.inserted, item)
	f.dueOns = append(f.dueOns, dueOn)
	return int64(len(f.inserted)), nil
}

func (f *fakeLearning) AutoResolve(_ context.Context, cutoffDay, resolveDay time.Time, ladder []int) (int, error) {
	f.autoResolves = append(f.autoResolves, autoResolveCall{cutoffDay: cutoffDay, resolveDay: resolveDay, ladder: ladder})
	return f.autoResolveN, f.autoResolveErr
}

func (f *fakeLearning) ListDue(_ context.Context, day time.Time, limit int) ([]learning.Item, error) {
	f.listDueDays = append(f.listDueDays, day)
	f.listDueLimit = limit
	if f.dueErr != nil {
		return nil, f.dueErr
	}
	return f.due, nil
}

func (f *fakeLearning) RecordAsked(_ context.Context, itemIDs []int64, episodeID int64, askedOn time.Time) error {
	if f.recordAskedErr != nil {
		return f.recordAskedErr
	}
	f.asked = append(f.asked, recordAskedCall{itemIDs: itemIDs, episodeID: episodeID, askedOn: askedOn})
	return nil
}

// validWav is a syntactically valid PCM wav for the fake synthesizer — the
// quiz corner derives its silence format from real output bytes (§12-5).
func validWav() []byte {
	wav, err := tts.SilenceWav(tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 24000, BitsPerSample: 16}, 50*time.Millisecond)
	if err != nil {
		panic(err)
	}
	return wav
}

type fakeTTS struct {
	err error
	// failSubstring makes SynthesizeScript fail only for scripts containing
	// it (私的側だけ落とす縮退テスト用; e.g. the quiz lead's 「復習のコーナー」).
	failSubstring  string
	calls          int
	scripts        []string
	speakerName    string // "" = "ずんだもん"
	speakerNameErr error
}

func (f *fakeTTS) SynthesizeScript(_ context.Context, script string) ([]tts.Audio, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.failSubstring != "" && strings.Contains(script, f.failSubstring) {
		return nil, errors.New("synthesizer down for this script")
	}
	f.scripts = append(f.scripts, script)
	return []tts.Audio{{Data: validWav(), Duration: 30 * time.Second}}, nil
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

type encodeCall struct {
	outPath  string
	wavPaths []string
	tags     tts.ID3
}

type fakeEncoder struct {
	err   error
	calls []encodeCall
}

func (f *fakeEncoder) ConcatToMP3(_ context.Context, wavPaths []string, outPath string, tags tts.ID3) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, encodeCall{outPath: outPath, wavPaths: wavPaths, tags: tags})
	return os.WriteFile(outPath, []byte("mp3-bytes"), 0o600)
}

type fakeTransfer struct {
	err error
	// failOnCall fails the n-th Transfer (1-based); 0 = never.
	failOnCall int
	calls      int
	filenames  []string
	localPaths []string
}

func (f *fakeTransfer) Transfer(_ context.Context, localPath, filename string) (string, error) {
	f.calls++
	if f.err != nil || (f.failOnCall > 0 && f.calls == f.failOnCall) {
		if f.err != nil {
			return "", f.err
		}
		return "", errors.New("rsync: connection unexpectedly closed")
	}
	f.filenames = append(f.filenames, filename)
	f.localPaths = append(f.localPaths, localPath)
	return "/data/episodes/" + filename, nil
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

func (d *deps) publicCreated() (*entity.Episode, []*entity.Segment) {
	return d.episodes.byKind(entity.FeedKindPublic)
}

func (d *deps) privateCreated() (*entity.Episode, []*entity.Segment) {
	return d.episodes.byKind(entity.FeedKindPrivate)
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 5, 4, 30, 0, 0, time.UTC)
}

func newPipeline(t *testing.T, d *deps) *radio.Pipeline {
	t.Helper()
	cfg := radio.Config{
		ShowName:          "pulse",
		MaxArticles:       8,
		Location:          time.UTC,
		EpisodesDir:       "/data/episodes",
		LearningURL:       "https://pulse.catchup-feed.com/learning",
		BookReviewChunks:  3,
		PrivateEpisodeMax: 18 * time.Minute,
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
		episodes: &fakeEpisodes{nextID: 55, todayCounts: map[string]int{}},
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
	ep, segs := d.publicCreated()
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
	require.Len(t, segs, 4)

	// jobs for the Pi worker (§6-5, C-4)
	require.Len(t, d.jobs.jobs, 2)
	assert.Equal(t, entity.JobKindRegenerateFeed, d.jobs.jobs[0].kind)
	assert.Equal(t, entity.JobKindNotifyEpisode, d.jobs.jobs[1].kind)
	assert.JSONEq(t, `{"episode_id":55}`, string(d.jobs.jobs[1].payload))

	// encoding got every wav and the ID3 tags (§6-4)
	require.Len(t, d.encoder.calls, 1, "no LearningStore → public episode only (pre-Phase 3 behavior)")
	assert.Len(t, d.encoder.calls[0].wavPaths, 4)
	assert.Equal(t, "pulse 2026-07-05", d.encoder.calls[0].tags.Title)
	assert.Equal(t, "2026-07-05", d.encoder.calls[0].tags.Date)
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
	ep1, _ := run1.publicCreated()
	require.NotNil(t, ep1)
	require.True(t, ep1.PublishedAt.Equal(t0),
		"run 1 must record its selection timestamp as published_at")

	// Run 2: the previous public episode is run 1's row.
	run2 := defaultDeps()
	run2.episodes.previous = []*entity.Episode{ep1}
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
	assert.Empty(t, d.episodes.created)
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
			d.episodes.todayCounts[entity.FeedKindPublic] = tt.todayCount
			require.NoError(t, newPipeline(t, d).Run(context.Background(), radio.RunOptions{}))

			ep, _ := d.publicCreated()
			require.NotNil(t, ep)
			assert.Equal(t, tt.wantTitle, ep.Title, "§6-6: rev 付き新規版")
			assert.Equal(t, tt.wantAudio, ep.AudioPath,
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
			assert.Empty(t, d.episodes.created, "failed run must not insert an episode (§6-6)")
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
	assert.Empty(t, d.episodes.created, "dry-run writes nothing to the DB")
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

	assert.Empty(t, d.episodes.created)
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
	ep, _ := d.publicCreated()
	assert.Contains(t, ep.ShowNotes, "https://example.com/a",
		"overflow article still appears in the show notes (§6-1)")
	assert.Contains(t, ep.ShowNotes, "紹介しきれなかった記事")
}

// ---- Phase 3 手順2: 学習項目の相乗り生成 (§5.1/§5.2) ----

func defaultLearningCfg() learning.Config {
	return learning.Config{
		ItemsPerDay:           1,
		Ladder:                []int{1, 7, 30},
		Slots:                 4,
		BackpressureThreshold: 30,
		AutoResolveAfter:      48 * time.Hour,
	}
}

func sampleDrafts() []script.QuizDraft {
	return []script.QuizDraft{{
		ArticleID: 10, Concept: "見出し",
		Question: "昨日のニュースで触れた件ですが。", Answer: "こうです。",
		Provider: "groq",
	}}
}

func learningPipeline(t *testing.T, d *deps, l *fakeLearning) *radio.Pipeline {
	t.Helper()
	p := newPipeline(t, d)
	p.Learning = l
	p.LearningCfg = defaultLearningCfg()
	return p
}

// dueItems returns two due items: one article-derived, one book-derived —
// the corner treats them alike (§5.3: 出題側は kind を区別しない).
func sampleDueItems() []learning.Item {
	articleID := int64(77)
	bookID := int64(3)
	day := learning.BroadcastDay(fixedNow())
	return []learning.Item{
		{ID: 101, Kind: learning.KindArticle, ArticleID: &articleID,
			Concept: "コンテキスト伝播", Question: "問いその1?", Answer: "答えその1。",
			Stage: 1, DueOn: day.AddDate(0, 0, -1)},
		{ID: 102, Kind: learning.KindBook, BookID: &bookID,
			Concept: "章の学び", Question: "問いその2?", Answer: "答えその2。",
			Stage: 0, DueOn: day},
	}
}

// TestPipeline_Run_LearningItemInsert covers the §5.1 happy path: M rides
// on the script call, and the parsed drafts land in learning_items only
// AFTER the broadcast is committed — stage 0 / due_on = 翌放送日 is the
// repository's job; the pipeline pins the JST due day and the passthrough
// of the actually-responding provider. overdue == threshold pins the
// strict comparison (§5.2: 閾値「超過」で停止、ちょうどは継続).
func TestPipeline_Run_LearningItemInsert(t *testing.T) {
	d := defaultDeps()
	d.script.drafts = sampleDrafts()
	l := &fakeLearning{overdue: 30}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	assert.Equal(t, 1, d.script.quizCount, "M=1 rides on the script call (D-19)")

	// fixedNow (2026-07-05 04:30 UTC = 13:30 JST) → 放送日 7/5、翌日 7/6.
	day := learning.BroadcastDay(fixedNow())
	require.Len(t, l.countDays, 1)
	assert.True(t, l.countDays[0].Equal(day), "backpressure input is the JST broadcast day (§12-10)")
	require.Len(t, l.hasDays, 1)
	assert.True(t, l.hasDays[0].Equal(day))

	require.Len(t, l.inserted, 1)
	item := l.inserted[0]
	assert.Equal(t, learning.KindArticle, item.Kind)
	require.NotNil(t, item.ArticleID)
	assert.Equal(t, int64(10), *item.ArticleID)
	assert.Nil(t, item.BookID)
	assert.Equal(t, "groq", item.Provider, "provider is the LLM that actually answered, not the chain head")
	assert.Equal(t, "見出し", item.Concept)
	require.Len(t, l.dueOns, 1)
	assert.True(t, l.dueOns[0].Equal(learning.FirstDueDay(fixedNow())),
		"due_on = 翌放送日 — 当日のクイズコーナーには出さない (§5.1)")
	assert.Equal(t, "2026-07-06", learning.FormatDay(l.dueOns[0]))

	// 公開エピソード側は完全不変 (§12-1)。
	ep, segs := d.publicCreated()
	require.NotNil(t, ep)
	assert.Equal(t, entity.FeedKindPublic, ep.FeedKind)
	require.Len(t, segs, 4)
	assert.Len(t, d.jobs.jobs, 2)
}

// TestPipeline_Run_LearningGenerationSuppressed pins every path that must
// zero out M before the LLM call (§5.2: プロンプト側で抑止 — トークンも
// 使わない), while the broadcast itself proceeds untouched.
func TestPipeline_Run_LearningGenerationSuppressed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeLearning, *radio.Pipeline)
	}{
		{
			name: "backpressure over threshold (§5.2)",
			mutate: func(l *fakeLearning, _ *radio.Pipeline) {
				l.overdue = 31
			},
		},
		{
			name: "backpressure check fails (§9: 縮退)",
			mutate: func(l *fakeLearning, _ *radio.Pipeline) {
				l.overdueErr = errors.New("pg down")
			},
		},
		{
			name: "items already generated today (same-day rev re-run, §12-2)",
			mutate: func(l *fakeLearning, _ *radio.Pipeline) {
				l.hasToday = true
			},
		},
		{
			name: "dedupe check fails (§9: 縮退)",
			mutate: func(l *fakeLearning, _ *radio.Pipeline) {
				l.hasTodayErr = errors.New("pg down")
			},
		},
		{
			name: "items per day configured to zero",
			mutate: func(_ *fakeLearning, p *radio.Pipeline) {
				p.LearningCfg.ItemsPerDay = 0
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := defaultDeps()
			d.script.drafts = sampleDrafts()
			l := &fakeLearning{}
			p := learningPipeline(t, d, l)
			tt.mutate(l, p)

			require.NoError(t, p.Run(context.Background(), radio.RunOptions{}),
				"item generation must never stop the broadcast (§9)")

			assert.Equal(t, 0, d.script.quizCount,
				"suppression must happen on the prompt side, before the LLM call")
			assert.Empty(t, l.inserted)
			ep, _ := d.publicCreated()
			require.NotNil(t, ep, "the public episode ships regardless")
			assert.Len(t, d.jobs.jobs, 2)
		})
	}
}

// TestPipeline_Run_LearningStoreAbsent: a pipeline without a LearningStore
// (older callers, tests) behaves exactly as before Phase 3 — no item
// generation and no private twin.
func TestPipeline_Run_LearningStoreAbsent(t *testing.T) {
	d := defaultDeps()
	d.script.drafts = sampleDrafts()

	require.NoError(t, newPipeline(t, d).Run(context.Background(), radio.RunOptions{}))

	assert.Equal(t, 0, d.script.quizCount)
	require.Len(t, d.episodes.created, 1, "public episode only")
	assert.Equal(t, entity.FeedKindPublic, d.episodes.created[0].FeedKind)
}

// TestPipeline_Run_LearningInsertFailureKeepsBroadcast pins §5.1/§9: the
// INSERT is best-effort — a dead DB at the very end loses the day's items
// (遡り生成はしない) but the run still reports success because the episode
// is already on the Pi and registered.
func TestPipeline_Run_LearningInsertFailureKeepsBroadcast(t *testing.T) {
	d := defaultDeps()
	d.script.drafts = sampleDrafts()
	l := &fakeLearning{insertErr: errors.New("pg down")}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	assert.Empty(t, l.inserted)
	ep, _ := d.publicCreated()
	require.NotNil(t, ep)
	assert.Len(t, d.jobs.jobs, 2, "regenerate_feed / notify_episode are unaffected")
}

// TestPipeline_Run_LearningNoDrafts: a quiz-side degradation inside the
// generator (marker missing, unparseable section) surfaces as zero drafts
// — the pipeline inserts nothing and the broadcast is untouched (§5.1).
func TestPipeline_Run_LearningNoDrafts(t *testing.T) {
	d := defaultDeps()
	d.script.drafts = nil // generator degraded to "no items today"
	l := &fakeLearning{}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	assert.Equal(t, 1, d.script.quizCount, "generation was requested")
	assert.Empty(t, l.inserted)
	ep, _ := d.publicCreated()
	require.NotNil(t, ep)
}

// TestPipeline_Run_LearningNotInsertedOnBroadcastFailure pins the ordering
// contract (§5.1): items exist only for articles that actually went on
// air. Any failure before the episode row is committed must leave
// learning_items untouched, no matter how far the drafts got.
func TestPipeline_Run_LearningNotInsertedOnBroadcastFailure(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*deps)
	}{
		{"TTS failure", func(d *deps) { d.tts.err = errors.New("connection refused") }},
		{"encode failure", func(d *deps) { d.encoder.err = errors.New("exit status 1") }},
		{"transfer failure", func(d *deps) { d.transfer.err = errors.New("rsync down") }},
		{"episode insert failure", func(d *deps) { d.episodes.createErr = errors.New("unique violation") }},
		{"job enqueue failure", func(d *deps) { d.jobs.err = errors.New("pg down") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := defaultDeps()
			d.script.drafts = sampleDrafts()
			tt.mutate(d)
			l := &fakeLearning{}
			p := learningPipeline(t, d, l)

			require.Error(t, p.Run(context.Background(), radio.RunOptions{}))
			assert.Empty(t, l.inserted,
				"放送されなかった記事を学習項目化してはならない (§5.1)")
		})
	}
}

// TestPipeline_Run_DryRunLearning: dry-run exercises the full prompt path
// (D-2: プロンプト調整用) and prints the drafts, but writes nothing —
// InsertItem included.
func TestPipeline_Run_DryRunLearning(t *testing.T) {
	d := defaultDeps()
	d.script.drafts = sampleDrafts()
	l := &fakeLearning{}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{DryRun: true}))

	assert.Equal(t, 1, d.script.quizCount, "dry-run still renders the quiz section for tuning")
	assert.Empty(t, l.inserted, "dry-run writes nothing to the DB")
	assert.Empty(t, d.episodes.created)

	printed := d.out.String()
	assert.Contains(t, printed, "learning item 1 (dry-run, not inserted)")
	assert.Contains(t, printed, "見出し")
	assert.Contains(t, printed, "昨日のニュースで触れた件ですが。")
}

// ---- Phase 3 手順3: 私的エピソード二本立て+クイズ注入 (§7.1/§7.2) ----

// TestPipeline_Run_PrivateTwin covers the §7.1 happy path on a news day:
// the public episode ships exactly as before, then the private twin ships
// with the quiz corner injected between the last news and the outro, the
// news wavs shared, the §7.5 show-notes section appended, and the asked
// items recorded against the PRIVATE episode's id.
func TestPipeline_Run_PrivateTwin(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	day := learning.BroadcastDay(fixedNow())

	// §3 手順1: 自動解決は選定より前に1回、cutoff は 48h 前の放送日。
	require.Len(t, l.autoResolves, 1)
	assert.True(t, l.autoResolves[0].cutoffDay.Equal(learning.BroadcastDay(fixedNow().Add(-48*time.Hour))))
	assert.True(t, l.autoResolves[0].resolveDay.Equal(day))
	assert.Equal(t, []int{1, 7, 30}, l.autoResolves[0].ladder)

	// §6.3: 選定は放送日+出題枠 S。
	require.Len(t, l.listDueDays, 1)
	assert.True(t, l.listDueDays[0].Equal(day))
	assert.Equal(t, 4, l.listDueLimit)

	// 公開エピソードは完全不変 (§12-1)。
	pub, pubSegs := d.publicCreated()
	require.NotNil(t, pub)
	assert.Equal(t, "pulse 2026-07-05", pub.Title)
	assert.Equal(t, "/data/episodes/2026-07-05.mp3", pub.AudioPath)
	assert.Equal(t, 120, pub.DurationSec, "公開の尺は 4 セグメント × 30s のまま")
	require.Len(t, pubSegs, 4)
	assert.NotContains(t, pub.ShowNotes, "今日の復習",
		"§10: 学習コンテンツは公開ショーノートに現れない")
	assert.NotContains(t, pub.ShowNotes, "コンテキスト伝播")

	// 私的エピソード: -private 命名、published_at は公開版と同一時刻。
	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv)
	assert.Equal(t, "pulse 2026-07-05", priv.Title)
	assert.Equal(t, "/data/episodes/2026-07-05-private.mp3", priv.AudioPath)
	assert.True(t, priv.PublishedAt.Equal(pub.PublishedAt),
		"同一の選定時刻 — 私的フィードのペア畳み込みの対応キー")

	// 尺: news 120s + lead 30s + (question 30s + 無音 3s + answer 30s) × 2.
	assert.Equal(t, 120+30+2*63, priv.DurationSec)

	// セグメント: intro, news×2, quiz lead, quiz×2 (1項目=1行), outro。
	require.Len(t, privSegs, 7)
	kinds := make([]string, len(privSegs))
	for i, s := range privSegs {
		kinds[i] = s.Kind
		assert.Equal(t, i+1, s.Position, "positions renumbered contiguously")
	}
	assert.Equal(t, []string{"intro", "news", "news", "quiz", "quiz", "quiz", "outro"}, kinds)
	assert.Contains(t, privSegs[3].Script, "復習のコーナー")
	assert.Contains(t, privSegs[3].Script, "2問", "つなぎ文は項目数を埋め込む (§7.2-1)")
	require.NotNil(t, privSegs[4].ArticleID, "記事由来の項目は article_id を持つ (§7.2-4)")
	assert.Equal(t, int64(77), *privSegs[4].ArticleID)
	assert.Contains(t, privSegs[4].Script, "問いその1?")
	assert.Contains(t, privSegs[4].Script, "答えその1。")
	assert.Nil(t, privSegs[5].ArticleID, "書籍由来の項目に article_id はない")

	// 私的ショーノート: §7.5 の concept 一覧+採点リンク+U-13 クレジット。
	assert.Contains(t, priv.ShowNotes, "今日の復習")
	assert.Contains(t, priv.ShowNotes, "コンテキスト伝播")
	assert.Contains(t, priv.ShowNotes, "章の学び")
	assert.Contains(t, priv.ShowNotes, "https://pulse.catchup-feed.com/learning")
	assert.True(t, strings.HasSuffix(priv.ShowNotes, "音声合成: VOICEVOX:ずんだもん"),
		"U-13: クレジット無し配信のパスは私的版にも存在しない")

	// concat リスト2通り: 公開はニュースのみ、私的は outro 直前に
	// lead → (question → 無音 → answer)×2 が挟まる (§7.2)。
	require.Len(t, d.encoder.calls, 2)
	pubWavs := d.encoder.calls[0].wavPaths
	privWavs := d.encoder.calls[1].wavPaths
	require.Len(t, pubWavs, 4)
	for _, w := range pubWavs {
		assert.NotContains(t, filepath.Base(w), "quiz",
			"§12-1: 公開 concat リストにクイズ素材が混ざらない")
	}
	require.Len(t, privWavs, 4+1+2*3)
	assert.Equal(t, pubWavs[:3], privWavs[:3], "news wav は公開版と共用 (§7.1)")
	assert.Equal(t, "quiz_lead_000.wav", filepath.Base(privWavs[3]))
	assert.Equal(t, "quiz_000_q_000.wav", filepath.Base(privWavs[4]))
	assert.Equal(t, "quiz_silence.wav", filepath.Base(privWavs[5]), "無音は独立 wav (§12-5)")
	assert.Equal(t, "quiz_000_a_000.wav", filepath.Base(privWavs[6]))
	assert.Equal(t, "quiz_silence.wav", filepath.Base(privWavs[8]), "同じ無音ファイルを再利用")
	assert.Equal(t, pubWavs[3], privWavs[len(privWavs)-1], "outro は最後")

	// §12-7: 通知は公開版のみ — ジョブは2件のまま、payload は公開版の id。
	require.Len(t, d.jobs.jobs, 2)
	assert.JSONEq(t, fmt.Sprintf(`{"episode_id":%d}`, pub.ID), string(d.jobs.jobs[1].payload))

	// §6.3: RecordAsked は私的エピソード確定後、私的版の id で1回。
	require.Len(t, l.asked, 1)
	assert.Equal(t, []int64{101, 102}, l.asked[0].itemIDs)
	assert.Equal(t, priv.ID, l.asked[0].episodeID)
	assert.True(t, l.asked[0].askedOn.Equal(day))
}

// TestPipeline_Run_PrivateTwinRevNaming pins the per-kind rev counting:
// the private filename family never collides with the public one.
func TestPipeline_Run_PrivateTwinRevNaming(t *testing.T) {
	d := defaultDeps()
	d.episodes.todayCounts[entity.FeedKindPublic] = 1
	d.episodes.todayCounts[entity.FeedKindPrivate] = 1
	l := &fakeLearning{due: sampleDueItems()}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	pub, _ := d.publicCreated()
	priv, _ := d.privateCreated()
	require.NotNil(t, pub)
	require.NotNil(t, priv)
	assert.Equal(t, "/data/episodes/2026-07-05-rev2.mp3", pub.AudioPath)
	assert.Equal(t, "/data/episodes/2026-07-05-rev2-private.mp3", priv.AudioPath)
	assert.Equal(t, "pulse 2026-07-05 rev2", priv.Title)
}

// TestPipeline_Run_PrivateTwinNoDueItems: the twin ships daily even with an
// empty selection (§7.1: 私的エピソードは毎日) — news-only, no quiz rows,
// no RecordAsked, no learning section in the notes.
func TestPipeline_Run_PrivateTwinNoDueItems(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv, "due ゼロでも私的版は出る(本人の購読先は私的フィード)")
	require.Len(t, privSegs, 4, "quiz セグメントなし")
	assert.Equal(t, 120, priv.DurationSec)
	assert.NotContains(t, priv.ShowNotes, "今日の復習")
	assert.Empty(t, l.asked)
}

// TestPipeline_Run_PrivateFailureKeepsPublic pins the §9 direction: any
// private-side failure abandons the twin only — the run reports success,
// the public episode and its jobs are untouched, and no review log is
// recorded for a corner nobody can hear.
func TestPipeline_Run_PrivateFailureKeepsPublic(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*deps)
	}{
		{"quiz corner TTS failure", func(d *deps) { d.tts.failSubstring = "復習のコーナー" }},
		{"private transfer failure", func(d *deps) { d.transfer.failOnCall = 2 }},
		{"private insert failure", func(d *deps) { d.episodes.privateCreateErr = errors.New("pg down") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := defaultDeps()
			tt.mutate(d)
			l := &fakeLearning{due: sampleDueItems()}
			p := learningPipeline(t, d, l)

			require.NoError(t, p.Run(context.Background(), radio.RunOptions{}),
				"縮退方向: 公開版は出す、私的版のみ諦める")

			pub, _ := d.publicCreated()
			require.NotNil(t, pub)
			priv, _ := d.privateCreated()
			assert.Nil(t, priv)
			assert.Len(t, d.jobs.jobs, 2, "公開側のジョブは不変")
			assert.Empty(t, l.asked, "放送されなかった出題は記録しない")
		})
	}
}

// TestPipeline_Run_PrivateRecordAskedFailure: the episode is already
// shipped, so a RecordAsked failure only warns — the items stay due and
// re-ask tomorrow (§9 self-healing).
func TestPipeline_Run_PrivateRecordAskedFailure(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems(), recordAskedErr: errors.New("pg down")}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	priv, _ := d.privateCreated()
	require.NotNil(t, priv, "出題記録の失敗はエピソードを取り消さない")
	assert.Empty(t, l.asked)
}

// TestPipeline_Run_ListDueFailureDegradesToNewsOnly: a selection failure
// costs the corner, not the twin and never the public episode (§9).
func TestPipeline_Run_ListDueFailureDegradesToNewsOnly(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{dueErr: errors.New("pg down")}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	pub, _ := d.publicCreated()
	require.NotNil(t, pub)
	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv)
	assert.Len(t, privSegs, 4, "quiz なしの news-only 私的版")
	assert.Empty(t, l.asked)
}

// TestPipeline_Run_AutoResolveFailureContinues: a failed auto-resolve only
// warns — asking continues (ListDue's stale-log exclusion prevents any
// double-ask), and the whole run is unaffected (§9).
func TestPipeline_Run_AutoResolveFailureContinues(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems(), autoResolveErr: errors.New("pg down")}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	require.Len(t, l.autoResolves, 1, "attempted once")
	require.Len(t, l.listDueDays, 1, "selection still ran")
	priv, _ := d.privateCreated()
	require.NotNil(t, priv)
	assert.Len(t, l.asked, 1)
}

// TestPipeline_Run_DryRunQuizSelection: dry-run prints the selection but
// performs no learning write — no AutoResolve (it mutates state), no
// RecordAsked, no episodes.
func TestPipeline_Run_DryRunQuizSelection(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{DryRun: true}))

	assert.Empty(t, l.autoResolves, "dry-run は AutoResolve を書かない")
	assert.Empty(t, l.asked, "dry-run は RecordAsked を書かない")
	assert.Empty(t, d.episodes.created)
	assert.Equal(t, 0, d.tts.calls)

	printed := d.out.String()
	assert.Contains(t, printed, "quiz selection: 2 item(s) due")
	assert.Contains(t, printed, "コンテキスト伝播")
}

// ---- Phase 3 手順3: 記事ゼロ日の私的版のみ生成 (§7.1/§12-8) ----

// TestPipeline_Run_QuizOnlyDay: no articles + due items → a private-only
// episode from fixed templates (LLM は呼ばない), quiz corner between the
// fixed intro and outro, jobs empty (通知なし §12-7), asked recorded.
func TestPipeline_Run_QuizOnlyDay(t *testing.T) {
	d := defaultDeps()
	d.articles.articles = nil
	l := &fakeLearning{due: sampleDueItems()[:1]}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}),
		"§7.1: 記事ゼロでも期日到来項目があれば私的版のみ生成")

	assert.False(t, d.script.called, "記事ゼロ日は LLM を呼ばない(固定テンプレート台本)")
	pub, _ := d.publicCreated()
	assert.Nil(t, pub, "公開版は D-1 どおりスキップ")

	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv)
	assert.Equal(t, "pulse 2026-07-05", priv.Title)
	assert.Equal(t, "/data/episodes/2026-07-05-private.mp3", priv.AudioPath)
	// intro 30s + lead 30s + (q 30s + 無音 3s + a 30s) + outro 30s.
	assert.Equal(t, 30+30+63+30, priv.DurationSec)

	require.Len(t, privSegs, 4)
	assert.Equal(t, entity.SegmentKindIntro, privSegs[0].Kind)
	assert.Contains(t, privSegs[0].Script, "今日は新しい記事のお知らせはありません")
	assert.Equal(t, entity.SegmentKindQuiz, privSegs[1].Kind)
	assert.Equal(t, entity.SegmentKindQuiz, privSegs[2].Kind)
	assert.Equal(t, entity.SegmentKindOutro, privSegs[3].Kind)
	assert.Contains(t, privSegs[3].Script, "今日のおさらいは以上です")

	assert.Contains(t, priv.ShowNotes, "今日の復習")
	assert.Contains(t, priv.ShowNotes, "https://pulse.catchup-feed.com/learning")
	assert.True(t, strings.HasSuffix(priv.ShowNotes, "音声合成: VOICEVOX:ずんだもん"), "U-13")

	// concat: intro → lead → q → 無音 → a → outro.
	require.Len(t, d.encoder.calls, 1)
	wavs := d.encoder.calls[0].wavPaths
	require.Len(t, wavs, 6)
	assert.Equal(t, "seg_000_000.wav", filepath.Base(wavs[0]))
	assert.Equal(t, "quiz_silence.wav", filepath.Base(wavs[3]))
	assert.Equal(t, "seg_001_000.wav", filepath.Base(wavs[5]))

	assert.Empty(t, d.jobs.jobs, "§12-7: 私的版はジョブを積まない(通知・フィード再生成とも)")
	require.Len(t, l.asked, 1)
	assert.Equal(t, priv.ID, l.asked[0].episodeID)
	require.Len(t, l.autoResolves, 1, "自動解決は記事ゼロ日でも選定前に走る")
}

// TestPipeline_Run_QuizOnlyDaySkips pins that the D-1 public skip contract
// is untouched when there is nothing due (or no learning store at all).
func TestPipeline_Run_QuizOnlyDaySkips(t *testing.T) {
	t.Run("no due items", func(t *testing.T) {
		d := defaultDeps()
		d.articles.articles = nil
		l := &fakeLearning{}
		p := learningPipeline(t, d, l)

		require.ErrorIs(t, p.Run(context.Background(), radio.RunOptions{}), radio.ErrNoArticles)
		assert.Empty(t, d.episodes.created)
		assert.Len(t, l.autoResolves, 1, "自動解決だけは走る(選定前、§3 手順1)")
	})

	t.Run("learning store absent", func(t *testing.T) {
		d := defaultDeps()
		d.articles.articles = nil

		require.ErrorIs(t, newPipeline(t, d).Run(context.Background(), radio.RunOptions{}), radio.ErrNoArticles)
		assert.Empty(t, d.episodes.created)
	})
}

// TestPipeline_Run_QuizOnlyDayFailure: on a quiz-only day the private
// episode IS the run's product, so a failure is a run failure (notify_error
// 経由で管理者に届く) and writes nothing.
func TestPipeline_Run_QuizOnlyDayFailure(t *testing.T) {
	d := defaultDeps()
	d.articles.articles = nil
	d.tts.err = errors.New("connection refused")
	l := &fakeLearning{due: sampleDueItems()[:1]}
	p := learningPipeline(t, d, l)

	require.Error(t, p.Run(context.Background(), radio.RunOptions{}))
	assert.Empty(t, d.episodes.created)
	assert.Empty(t, l.asked)
}

// TestPipeline_Run_QuizOnlyDayDryRun prints the would-be quiz-only episode
// and writes nothing.
func TestPipeline_Run_QuizOnlyDayDryRun(t *testing.T) {
	d := defaultDeps()
	d.articles.articles = nil
	l := &fakeLearning{due: sampleDueItems()[:1]}
	p := learningPipeline(t, d, l)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{DryRun: true}))

	assert.Empty(t, d.episodes.created)
	assert.Empty(t, l.autoResolves)
	assert.Empty(t, l.asked)
	assert.Equal(t, 0, d.tts.calls)

	printed := d.out.String()
	assert.Contains(t, printed, "復習のコーナー")
	assert.Contains(t, printed, "quiz selection: 1 item(s) due")
}
