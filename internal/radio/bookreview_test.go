package radio_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/learning"
	"catchup-feed/internal/radio"
	"catchup-feed/internal/repository"
	"catchup-feed/internal/script"
)

// ---- book_review fakes (§7.3/§5.3) ----

type advanceCall struct {
	bookID   int64
	from, to int
	finished bool
}

// fakeBookReview implements radio.BookReviewStore and records every call.
type fakeBookReview struct {
	active      repository.ActiveReviewBook
	hasActive   bool
	activeErr   error
	chunks      []repository.BookReviewChunk
	chunksErr   error
	hasToday    bool
	hasTodayErr error
	advanceErr  error

	activeCalls int
	nextCursors []int
	hasDays     []time.Time
	advances    []advanceCall
}

func (f *fakeBookReview) ActiveBook(_ context.Context) (repository.ActiveReviewBook, bool, error) {
	f.activeCalls++
	return f.active, f.hasActive, f.activeErr
}

func (f *fakeBookReview) NextChunks(_ context.Context, _ int64, cursor, _ int) ([]repository.BookReviewChunk, error) {
	f.nextCursors = append(f.nextCursors, cursor)
	if f.chunksErr != nil {
		return nil, f.chunksErr
	}
	return f.chunks, nil
}

func (f *fakeBookReview) HasBookReviewOn(_ context.Context, day time.Time) (bool, error) {
	f.hasDays = append(f.hasDays, day)
	return f.hasToday, f.hasTodayErr
}

func (f *fakeBookReview) AdvanceCursor(_ context.Context, bookID int64, from, to int, finished bool) error {
	if f.advanceErr != nil {
		return f.advanceErr
	}
	f.advances = append(f.advances, advanceCall{bookID: bookID, from: from, to: to, finished: finished})
	return nil
}

type bookReviewGenCall struct {
	title  string
	chunks []script.BookChunk
}

// fakeBookReviewer implements radio.BookReviewer (the Ollama-only generator).
type fakeBookReviewer struct {
	result script.BookReviewResult
	err    error
	calls  []bookReviewGenCall
}

func (f *fakeBookReviewer) Generate(_ context.Context, title string, chunks []script.BookChunk) (script.BookReviewResult, error) {
	f.calls = append(f.calls, bookReviewGenCall{title: title, chunks: chunks})
	if f.err != nil {
		return script.BookReviewResult{}, f.err
	}
	return f.result, nil
}

// ---- book_review helpers ----

func activeBook() repository.ActiveReviewBook {
	return repository.ActiveReviewBook{ID: 7, Title: "Learning Go", Cursor: 3, TotalChunks: 180}
}

func sampleBookChunks() []repository.BookReviewChunk {
	return []repository.BookReviewChunk{
		{Position: 3, Content: "チャネルの話。"},
		{Position: 4, Content: "select の話。"},
		{Position: 5, Content: "context の話。"},
	}
}

func sampleBookResult() script.BookReviewResult {
	return script.BookReviewResult{
		Script: "今日は、いま読んでいる本のコーナーです。チャネルについてお話しします。",
		Quiz:   &script.BookQuizDraft{Concept: "章の学び", Question: "本の問い?", Answer: "本の答え。"},
	}
}

// activeBookReview returns a fake store ready to book-review from cursor 3.
func activeBookReview() *fakeBookReview {
	return &fakeBookReview{active: activeBook(), hasActive: true, chunks: sampleBookChunks()}
}

func bookReviewPipeline(t *testing.T, d *deps, l *fakeLearning, br *fakeBookReview, reviewer *fakeBookReviewer) *radio.Pipeline {
	t.Helper()
	p := learningPipeline(t, d, l)
	p.BookReview = br
	p.BookReviewLLM = reviewer
	return p
}

func segmentKinds(segs []*entity.Segment) []string {
	kinds := make([]string, len(segs))
	for i, s := range segs {
		kinds[i] = s.Kind
	}
	return kinds
}

// ---- tests ----

// TestPipeline_Run_BookReview_Injected covers the §7.3 happy path on a news
// day: the book_review segment is injected between the quiz corner and the
// outro (private only), the cursor advances AFTER the episode is stored, the
// §5.3 book quiz lands as a kind='book' provider='ollama' item, and the public
// episode stays completely unchanged (§12-1).
func TestPipeline_Run_BookReview_Injected(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	br := activeBookReview()
	reviewer := &fakeBookReviewer{result: sampleBookResult()}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	day := learning.BroadcastDay(fixedNow())

	// §12-4: book text only reaches the Ollama-only generator. It was called
	// once with the book title and the chunk contents.
	require.Len(t, reviewer.calls, 1)
	assert.Equal(t, "Learning Go", reviewer.calls[0].title)
	require.Len(t, reviewer.calls[0].chunks, 3)
	assert.Equal(t, "チャネルの話。", reviewer.calls[0].chunks[0].Content)

	// §12-2 dedupe read on the JST broadcast day; chunks read from the cursor.
	require.Len(t, br.hasDays, 1)
	assert.True(t, br.hasDays[0].Equal(day))
	assert.Equal(t, []int{3}, br.nextCursors, "chunks read from review_cursor (§7.3)")

	// 公開エピソードは完全不変 (§12-1): no book_review anywhere.
	pub, pubSegs := d.publicCreated()
	require.NotNil(t, pub)
	require.Len(t, pubSegs, 4)
	assert.NotContains(t, segmentKinds(pubSegs), entity.SegmentKindBookReview)
	for _, w := range d.encoder.calls[0].wavPaths {
		assert.NotContains(t, filepath.Base(w), "bookreview",
			"§12-1: 公開 concat リストに book_review 素材が混ざらない")
	}

	// 私的: intro, news×2, quiz lead, quiz×2, book_review, outro = 8 段。
	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv)
	require.Len(t, privSegs, 8)
	assert.Equal(t,
		[]string{"intro", "news", "news", "quiz", "quiz", "quiz", "book_review", "outro"},
		segmentKinds(privSegs))
	for i, s := range privSegs {
		assert.Equal(t, i+1, s.Position, "positions renumbered contiguously")
	}
	assert.Equal(t, sampleBookResult().Script, privSegs[6].Script, "book_review script は生成全文 (§4)")
	assert.Nil(t, privSegs[6].ArticleID, "book_review は記事に紐づかない")

	// duration = news 120 + corner 156 + book_review 30.
	assert.Equal(t, 120+30+2*63+30, priv.DurationSec)

	// concat: news(3) + corner(1+2*3) + book_review(1) + outro(1) = 12。
	privWavs := d.encoder.calls[1].wavPaths
	require.Len(t, privWavs, 3+7+1+1)
	assert.Equal(t, "bookreview_000.wav", filepath.Base(privWavs[10]), "book_review は quiz の後・outro の前")
	assert.Equal(t, d.encoder.calls[0].wavPaths[3], privWavs[11], "outro は最後")

	// §7.3 カーソル前進は私的エピソード確定後、cursor 3 -> 6、末尾未達。
	require.Len(t, br.advances, 1)
	assert.Equal(t, advanceCall{bookID: 7, from: 3, to: 6, finished: false}, br.advances[0])

	// §5.3 書籍クイズ: kind='book'、provider='ollama' 固定、due=翌日。
	require.Len(t, l.inserted, 1)
	item := l.inserted[0]
	assert.Equal(t, learning.KindBook, item.Kind)
	require.NotNil(t, item.BookID)
	assert.Equal(t, int64(7), *item.BookID)
	assert.Nil(t, item.ArticleID)
	assert.Equal(t, learning.ProviderOllama, item.Provider)
	assert.Equal(t, "章の学び", item.Concept)
	assert.True(t, l.dueOns[0].Equal(learning.FirstDueDay(fixedNow())))
}

// TestPipeline_Run_BookReview_NoActiveBook: no active book → book_review is
// skipped, the private episode is the plain news+quiz twin, and no cursor moves.
func TestPipeline_Run_BookReview_NoActiveBook(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	br := &fakeBookReview{hasActive: false}
	reviewer := &fakeBookReviewer{result: sampleBookResult()}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	_, privSegs := d.privateCreated()
	require.Len(t, privSegs, 7, "no book_review segment")
	assert.NotContains(t, segmentKinds(privSegs), entity.SegmentKindBookReview)
	assert.Empty(t, reviewer.calls, "no active book → no Ollama call")
	assert.Empty(t, br.advances)
	assert.Empty(t, l.inserted)
}

// TestPipeline_Run_BookReview_SameDayRevSkips pins §12-2: a same-day rev sees
// today's committed book_review segment (HasBookReviewOn) and skips — no
// regeneration, no double cursor advance, no double book-quiz insert.
func TestPipeline_Run_BookReview_SameDayRevSkips(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	br := activeBookReview()
	br.hasToday = true
	reviewer := &fakeBookReviewer{result: sampleBookResult()}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	_, privSegs := d.privateCreated()
	require.Len(t, privSegs, 7, "same-day rev: book_review already done, skipped")
	assert.Empty(t, reviewer.calls, "no regeneration on a same-day rev")
	assert.Empty(t, br.nextCursors, "cursor not even read once dedupe fires")
	assert.Empty(t, br.advances, "no double cursor advance (§12-2)")
	assert.Empty(t, l.inserted, "no double book-quiz insert")
}

// TestPipeline_Run_BookReview_LengthGuardDefers pins §7.1: when news+quiz plus
// the estimated book_review length would exceed the private-episode cap, the
// book_review is deferred to tomorrow — not generated and the cursor untouched
// (翌日同じ箇所から).
func TestPipeline_Run_BookReview_LengthGuardDefers(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	br := activeBookReview()
	reviewer := &fakeBookReviewer{result: sampleBookResult()}
	p := bookReviewPipeline(t, d, l, br, reviewer)
	p.Config.PrivateEpisodeMax = 5 * time.Minute // news 120s + corner 156s + 3min est > 5min

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	_, privSegs := d.privateCreated()
	require.Len(t, privSegs, 7, "book_review deferred: not in the episode")
	assert.Empty(t, reviewer.calls, "guard decides BEFORE generation — no Ollama/TTS spent (§7.1)")
	assert.Empty(t, br.advances, "deferred means the cursor does not move (翌日同じ箇所から)")
	assert.Empty(t, l.inserted)
}

// TestPipeline_Run_BookReview_GenerationFailsDegrades pins §7.3/§9: an Ollama
// (or TTS) failure drops book_review only — the private episode still ships
// news+quiz, the cursor does not advance, and no book quiz is written.
func TestPipeline_Run_BookReview_GenerationFailsDegrades(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	br := activeBookReview()
	reviewer := &fakeBookReviewer{err: errors.New("ollama down")}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv, "private twin still ships (§9)")
	require.Len(t, privSegs, 7)
	require.Len(t, reviewer.calls, 1, "generation was attempted")
	assert.Empty(t, br.advances, "failed generation must not advance the cursor (§7.3)")
	assert.Empty(t, l.inserted, "no book quiz on a failed book_review")
	// 出題ログは記録される(quiz corner は成立)。
	require.Len(t, l.asked, 1)
}

// TestPipeline_Run_BookReview_QuizNilShipsReview pins §5.3: a nil book quiz
// (unparseable) still ships the book_review and advances the cursor — only the
// quiz insert is skipped.
func TestPipeline_Run_BookReview_QuizNilShipsReview(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	br := activeBookReview()
	reviewer := &fakeBookReviewer{result: script.BookReviewResult{Script: "本の紹介のみ。", Quiz: nil}}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	_, privSegs := d.privateCreated()
	require.Len(t, privSegs, 8, "book_review still ships without a quiz (§5.3)")
	assert.Contains(t, segmentKinds(privSegs), entity.SegmentKindBookReview)
	require.Len(t, br.advances, 1, "cursor advances — the review was produced")
	assert.Empty(t, l.inserted, "no book quiz item when the quiz degraded to nil")
}

// TestPipeline_Run_BookReview_FinishesAtEnd pins §7.3: reaching the total
// chunk count sets finished on the cursor advance.
func TestPipeline_Run_BookReview_FinishesAtEnd(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	br := activeBookReview()
	br.active = repository.ActiveReviewBook{ID: 7, Title: "Learning Go", Cursor: 3, TotalChunks: 6}
	reviewer := &fakeBookReviewer{result: sampleBookResult()}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	require.Len(t, br.advances, 1)
	assert.Equal(t, advanceCall{bookID: 7, from: 3, to: 6, finished: true}, br.advances[0],
		"newCursor == total_chunks → finished (§7.3)")
}

// TestPipeline_Run_BookReview_ExhaustedActiveBookFinishes: an active book with
// no remaining chunks (empty NextChunks) is marked finished (a no-op cursor
// move) and no book_review ships.
func TestPipeline_Run_BookReview_ExhaustedActiveBookFinishes(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	br := &fakeBookReview{
		active:    repository.ActiveReviewBook{ID: 7, Title: "Done", Cursor: 180, TotalChunks: 180},
		hasActive: true,
		chunks:    nil, // exhausted
	}
	reviewer := &fakeBookReviewer{result: sampleBookResult()}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	_, privSegs := d.privateCreated()
	require.Len(t, privSegs, 7, "no book_review for an exhausted book")
	assert.Empty(t, reviewer.calls)
	require.Len(t, br.advances, 1)
	assert.Equal(t, advanceCall{bookID: 7, from: 180, to: 180, finished: true}, br.advances[0],
		"exhausted active book is finished with a no-op cursor move (§7.3)")
	assert.Empty(t, l.inserted)
}

// TestPipeline_Run_BookReview_DryRunNoWrites pins the dry-run contract: the
// book_review target is printed but nothing is generated, no cursor advances,
// no quiz is inserted.
func TestPipeline_Run_BookReview_DryRunNoWrites(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	br := activeBookReview()
	reviewer := &fakeBookReviewer{result: sampleBookResult()}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{DryRun: true}))

	out := d.out.String()
	assert.Contains(t, out, "book_review target")
	assert.Contains(t, out, "Learning Go")
	assert.Contains(t, out, "position 3..5")
	assert.Contains(t, out, "cursor 3 -> 6")

	assert.Empty(t, reviewer.calls, "dry-run does not call Ollama")
	assert.Empty(t, br.advances, "dry-run advances no cursor")
	assert.Empty(t, l.inserted, "dry-run inserts no book quiz")
	assert.Empty(t, d.episodes.created, "dry-run writes no episode")
}

// TestPipeline_Run_BookReview_BookOnlyDay pins §7.1/§12-8: no new articles and
// no due quiz, but an active book in progress → a private-only episode ships
// carrying just the book_review (intro → book_review → outro).
func TestPipeline_Run_BookReview_BookOnlyDay(t *testing.T) {
	d := defaultDeps()
	d.articles.articles = nil // 記事ゼロ
	l := &fakeLearning{}      // 期日到来項目もなし
	br := activeBookReview()
	reviewer := &fakeBookReviewer{result: sampleBookResult()}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}),
		"§7.1: 記事も期日もないが書籍進行中なら私的版のみ生成")

	pub, _ := d.publicCreated()
	assert.Nil(t, pub, "公開版は D-1 どおりスキップ")

	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv)
	assert.Equal(t, []string{"intro", "book_review", "outro"}, segmentKinds(privSegs))
	require.Len(t, reviewer.calls, 1)
	require.Len(t, br.advances, 1)
	assert.Equal(t, 3, br.advances[0].from)
	require.Len(t, l.inserted, 1, "book quiz still generated on a book-only day")
	assert.Equal(t, learning.KindBook, l.inserted[0].Kind)
}

// TestPipeline_Run_BookReview_BookOnlyDayGenerationFailSkips: a book-only day
// whose book_review generation fails has nothing to ship → clean skip (D-1),
// cursor untouched.
func TestPipeline_Run_BookReview_BookOnlyDayGenerationFailSkips(t *testing.T) {
	d := defaultDeps()
	d.articles.articles = nil
	l := &fakeLearning{}
	br := activeBookReview()
	reviewer := &fakeBookReviewer{err: errors.New("ollama down")}
	p := bookReviewPipeline(t, d, l, br, reviewer)

	err := p.Run(context.Background(), radio.RunOptions{})
	require.ErrorIs(t, err, radio.ErrNoArticles, "no articles, no quiz, book_review failed → skip")
	assert.Empty(t, d.episodes.created)
	assert.Empty(t, br.advances, "failed generation must not advance the cursor")
}

// TestPipeline_Run_BookReview_Disabled: a pipeline with Learning but no
// BookReview store leaves the private twin news+quiz only (pre-手順6 behavior).
func TestPipeline_Run_BookReview_Disabled(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems()}
	p := learningPipeline(t, d, l) // no BookReview / BookReviewLLM

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	_, privSegs := d.privateCreated()
	require.Len(t, privSegs, 7)
	assert.NotContains(t, segmentKinds(privSegs), entity.SegmentKindBookReview)
}
