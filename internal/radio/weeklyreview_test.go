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
)

// saturdayNow is a Saturday broadcast day (2026-07-11 04:30 UTC = JST 13:30
// 土曜) — the default QUIZ_WEEKLY_REVIEW_DOW (D-21). Every §7.4 test overrides
// Now to this so the weekly review actually fires.
func saturdayNow() time.Time {
	return time.Date(2026, 7, 11, 4, 30, 0, 0, time.UTC)
}

func sampleWeekly() learning.WeeklyReview {
	return learning.WeeklyReview{
		Concepts:       []string{"コンテキスト伝播", "select 文"},
		GraduatedCount: 1,
		Reintroduced:   "難しい概念",
	}
}

// TestPipeline_Run_WeeklyReview_Injected covers the §7.4 happy path on a
// Saturday news day: the review segment sits between the quiz corner and the
// outro (private only), the material query is called with the 7-day window
// start and the ladder length, the show notes carry the §7.5 weekly section,
// and the public episode is completely unchanged (§12-1).
func TestPipeline_Run_WeeklyReview_Injected(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems(), weekly: sampleWeekly()}
	p := learningPipeline(t, d, l)
	p.Now = saturdayNow

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	// material query: window start = 放送日-6, ladderLen = len(ladder).
	require.Len(t, l.weeklyFroms, 1)
	assert.True(t, l.weeklyFroms[0].Equal(learning.WeeklyReviewWindowStart(saturdayNow())),
		"§7.4: 直近7日の窓開始 (放送日-6)")
	require.Len(t, l.weeklyLadder, 1)
	assert.Equal(t, 3, l.weeklyLadder[0], "ladderLen = len([1,7,30]) — 卒業判定用")

	// 公開エピソードは完全不変 (§12-1): no review anywhere.
	pub, pubSegs := d.publicCreated()
	require.NotNil(t, pub)
	require.Len(t, pubSegs, 4)
	assert.NotContains(t, segmentKinds(pubSegs), entity.SegmentKindReview)
	assert.NotContains(t, pub.ShowNotes, "今週の学び")
	assert.NotContains(t, pub.ShowNotes, "コンテキスト伝播")
	for _, w := range d.encoder.calls[0].wavPaths {
		assert.NotContains(t, filepath.Base(w), "weeklyreview",
			"§12-1: 公開 concat リストに review 素材が混ざらない")
	}

	// 私的: intro, news×2, quiz lead, quiz×2, review, outro = 8 段。
	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv)
	require.Len(t, privSegs, 8)
	assert.Equal(t,
		[]string{"intro", "news", "news", "quiz", "quiz", "quiz", "review", "outro"},
		segmentKinds(privSegs))
	for i, s := range privSegs {
		assert.Equal(t, i+1, s.Position, "positions renumbered contiguously")
	}
	assert.Contains(t, privSegs[6].Script, "今週の学びを振り返って", "review script はテンプレート全文 (§4)")
	assert.Nil(t, privSegs[6].ArticleID, "review は記事に紐づかない")

	// duration = news 120 + corner 156 + review 30。
	assert.Equal(t, 120+30+2*63+30, priv.DurationSec)

	// concat: news(3) + corner(1+2*3) + review(1) + outro(1) = 12。
	privWavs := d.encoder.calls[1].wavPaths
	require.Len(t, privWavs, 3+7+1+1)
	assert.Equal(t, "weeklyreview_000.wav", filepath.Base(privWavs[10]), "review は quiz の後・outro の前")
	assert.Equal(t, d.encoder.calls[0].wavPaths[3], privWavs[11], "outro は最後")

	// §7.5 私的ショーノート: quiz セクション → 週次の学び → クレジット(末尾)。
	assert.Contains(t, priv.ShowNotes, "今日の復習")
	assert.Contains(t, priv.ShowNotes, "今週の学び:")
	assert.Contains(t, priv.ShowNotes, "- コンテキスト伝播")
	assert.Contains(t, priv.ShowNotes, "卒業した項目: 1件")
	assert.Contains(t, priv.ShowNotes, "もう一度おさらい: 難しい概念")

	// §12-7: 通知は公開版のみ。
	require.Len(t, d.jobs.jobs, 2)
}

// TestPipeline_Run_WeeklyReview_NotTheDay: on any other weekday the review is
// off — the material is never even queried, and the private twin is the plain
// news+quiz shape (fixedNow = 2026-07-05 は日曜).
func TestPipeline_Run_WeeklyReview_NotTheDay(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems(), weekly: sampleWeekly()}
	p := learningPipeline(t, d, l) // Now = fixedNow (Sunday)

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	assert.Empty(t, l.weeklyFroms, "曜日でない日は material を引かない (§7.4 ゲート)")
	_, privSegs := d.privateCreated()
	require.Len(t, privSegs, 7)
	assert.NotContains(t, segmentKinds(privSegs), entity.SegmentKindReview)
}

// TestPipeline_Run_WeeklyReview_EmptyWeekSkips: a Saturday with no material
// (nothing learned/graduated/forgotten) produces no review segment — 空の
// 振り返りを作らない (§7.4).
func TestPipeline_Run_WeeklyReview_EmptyWeekSkips(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems(), weekly: learning.WeeklyReview{}}
	p := learningPipeline(t, d, l)
	p.Now = saturdayNow

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	require.Len(t, l.weeklyFroms, 1, "material は引くが空だった")
	priv, privSegs := d.privateCreated()
	require.Len(t, privSegs, 7, "empty week → no review segment")
	assert.NotContains(t, segmentKinds(privSegs), entity.SegmentKindReview)
	assert.NotContains(t, priv.ShowNotes, "今週の学び")
}

// TestPipeline_Run_WeeklyReview_LookupFailDegrades: a material lookup error
// drops the review only — the private twin still ships news+quiz (§9).
func TestPipeline_Run_WeeklyReview_LookupFailDegrades(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems(), weeklyErr: errors.New("pg down")}
	p := learningPipeline(t, d, l)
	p.Now = saturdayNow

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv, "review の失敗は私的版本体を落とさない (§9)")
	require.Len(t, privSegs, 7)
	assert.NotContains(t, segmentKinds(privSegs), entity.SegmentKindReview)
}

// TestPipeline_Run_WeeklyReview_TTSFailDegrades: a review-only TTS failure
// drops the review, the twin ships news+quiz, the public side is untouched.
func TestPipeline_Run_WeeklyReview_TTSFailDegrades(t *testing.T) {
	d := defaultDeps()
	d.tts.failSubstring = "今週の学びを振り返って" // review script のみ落とす
	l := &fakeLearning{due: sampleDueItems(), weekly: sampleWeekly()}
	p := learningPipeline(t, d, l)
	p.Now = saturdayNow

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	pub, _ := d.publicCreated()
	require.NotNil(t, pub, "公開版は不変")
	_, privSegs := d.privateCreated()
	require.Len(t, privSegs, 7, "review TTS 失敗 → review なしで私的版は出る (§9)")
	assert.NotContains(t, segmentKinds(privSegs), entity.SegmentKindReview)
}

// TestPipeline_Run_WeeklyReview_CountsIntoBookGuard pins the 手順6 申し送り:
// the review's measured length is added to the book_review §7.1 length guard
// input, so a private episode that fits without the review tips over the cap
// with it — book_review deferred, review kept.
func TestPipeline_Run_WeeklyReview_CountsIntoBookGuard(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems(), weekly: sampleWeekly()}
	br := activeBookReview()
	reviewer := &fakeBookReviewer{result: sampleBookResult()}
	p := bookReviewPipeline(t, d, l, br, reviewer)
	p.Now = saturdayNow
	// news 120 + corner 156 + review 30 = 306s; +180s est = 486s > 480s cap.
	// Without the review it would be 276+180 = 456s ≤ cap, so the deferral is
	// caused by counting the review in (手順6 申し送り).
	p.Config.PrivateEpisodeMax = 8 * time.Minute

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	_, privSegs := d.privateCreated()
	assert.Contains(t, segmentKinds(privSegs), entity.SegmentKindReview, "review は出す")
	assert.NotContains(t, segmentKinds(privSegs), entity.SegmentKindBookReview,
		"review 尺を算入した結果 book_review は翌日回し (§7.1)")
	assert.Empty(t, reviewer.calls, "guard は生成前に判断 — Ollama を呼ばない")
	assert.Empty(t, br.advances, "deferred: カーソルは動かない")
}

// TestPipeline_Run_WeeklyReview_QuizOnlyDay: no new articles, but due quiz +
// weekly material on a Saturday → the quiz-only private episode carries the
// review between the quiz corner and the outro (§7.4: 記事ゼロ日でも土曜なら挿入).
func TestPipeline_Run_WeeklyReview_QuizOnlyDay(t *testing.T) {
	d := defaultDeps()
	d.articles.articles = nil
	l := &fakeLearning{due: sampleDueItems()[:1], weekly: sampleWeekly()}
	p := learningPipeline(t, d, l)
	p.Now = saturdayNow

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	pub, _ := d.publicCreated()
	assert.Nil(t, pub, "公開版は D-1 どおりスキップ")

	priv, privSegs := d.privateCreated()
	require.NotNil(t, priv)
	assert.Equal(t,
		[]string{"intro", "quiz", "quiz", "review", "outro"},
		segmentKinds(privSegs), "記事ゼロ日でも review は quiz の後・outro の前")
	assert.Contains(t, priv.ShowNotes, "今週の学び:")
	require.Len(t, l.weeklyFroms, 1)
}

// TestPipeline_Run_WeeklyReview_NoEpisodeForReviewAlone pins the 手順7 裁量
// 判断: the weekly review never resurrects an otherwise-skipped day. No
// articles, no due quiz, no active book — even on a Saturday with material,
// the day cleanly skips (D-1). review は成立するエピソードに相乗りするのみ.
func TestPipeline_Run_WeeklyReview_NoEpisodeForReviewAlone(t *testing.T) {
	d := defaultDeps()
	d.articles.articles = nil
	l := &fakeLearning{weekly: sampleWeekly()} // due 空
	p := learningPipeline(t, d, l)
	p.Now = saturdayNow

	require.ErrorIs(t, p.Run(context.Background(), radio.RunOptions{}), radio.ErrNoArticles)
	assert.Empty(t, d.episodes.created, "review 単独ではエピソードを起こさない")
}

// TestPipeline_Run_WeeklyReview_DryRun: dry-run reads the material read-only
// and prints the would-be review, writing nothing.
func TestPipeline_Run_WeeklyReview_DryRun(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems(), weekly: sampleWeekly()}
	p := learningPipeline(t, d, l)
	p.Now = saturdayNow

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{DryRun: true}))

	assert.Empty(t, d.episodes.created)
	assert.Equal(t, 0, d.tts.calls, "dry-run は TTS しない(review 含む)")
	printed := d.out.String()
	assert.Contains(t, printed, "weekly review")
	assert.Contains(t, printed, "今週の学びを振り返って")
}

// TestPipeline_Run_WeeklyReview_ConfiguredWeekday: a non-default DOW config is
// honored — with WeeklyReviewDOW = Sunday the review fires on fixedNow (Sunday).
func TestPipeline_Run_WeeklyReview_ConfiguredWeekday(t *testing.T) {
	d := defaultDeps()
	l := &fakeLearning{due: sampleDueItems(), weekly: sampleWeekly()}
	p := learningPipeline(t, d, l) // Now = fixedNow (Sunday)
	p.LearningCfg.WeeklyReviewDOW = time.Sunday

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	require.Len(t, l.weeklyFroms, 1, "設定曜日(日曜)に一致 → review 実行")
	_, privSegs := d.privateCreated()
	assert.Contains(t, segmentKinds(privSegs), entity.SegmentKindReview)
}
