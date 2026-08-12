package radio_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/radio"
	"catchup-feed/internal/tts"
)

// ---- helpers ----

// captureLogs attaches a buffer-backed logger so the §8 degradation warning
// can be asserted rather than assumed.
func captureLogs(p *radio.Pipeline) *bytes.Buffer {
	var buf bytes.Buffer
	p.Logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &buf
}

// assertJingleWrapped checks the concat list opens with the opening jingle
// and closes with the ending one (§6-4).
func assertJingleWrapped(t *testing.T, wavs []string) {
	t.Helper()
	require.GreaterOrEqual(t, len(wavs), 2)
	assert.Equal(t, "jingle_opening.wav", filepath.Base(wavs[0]), "先頭はオープニング")
	assert.Equal(t, "jingle_ending.wav", filepath.Base(wavs[len(wavs)-1]), "末尾はエンディング")
}

// assertNoJingle checks the concat list carries no jingle at all (縮退時).
func assertNoJingle(t *testing.T, wavs []string) {
	t.Helper()
	for _, w := range wavs {
		assert.NotContains(t, filepath.Base(w), "jingle",
			"ジングルのデコードに失敗した run では concat に一切現れない")
	}
}

// episodeShape describes one of the three concat sites (§6-4): the public
// episode, its private twin, and the articles-less private-only episode.
type episodeShape struct {
	name string
	// setup arranges the day; it returns the pipeline to run.
	setup func(t *testing.T, d *deps) *radio.Pipeline
	// kinds selected from the created episodes, in encoder-call order.
	kinds []string
	// speechSec is the TTS-only length of each episode, in encoder-call order.
	speechSec []int
}

func episodeShapes() []episodeShape {
	return []episodeShape{
		{
			name: "公開エピソード単体 (Phase 3 無効)",
			setup: func(t *testing.T, d *deps) *radio.Pipeline {
				t.Helper()
				return newPipeline(t, d)
			},
			kinds:     []string{entity.FeedKindPublic},
			speechSec: []int{120},
		},
		{
			name: "公開 + 私的の二本立て (§7.1)",
			setup: func(t *testing.T, d *deps) *radio.Pipeline {
				t.Helper()
				return learningPipeline(t, d, &fakeLearning{due: sampleDueItems()})
			},
			kinds:     []string{entity.FeedKindPublic, entity.FeedKindPrivate},
			speechSec: []int{120, 120 + 30 + 2*63},
		},
		{
			name: "記事ゼロ日の私的版のみ (§7.1/§12-8)",
			setup: func(t *testing.T, d *deps) *radio.Pipeline {
				t.Helper()
				d.articles.articles = nil
				return learningPipeline(t, d, &fakeLearning{due: sampleDueItems()[:1]})
			},
			kinds:     []string{entity.FeedKindPrivate},
			speechSec: []int{30 + 30 + 63 + 30},
		},
	}
}

// ---- tests ----

// TestPipeline_Run_JingleWrapsEveryEpisode pins §6-4 across all three concat
// sites: every episode the batch produces opens and closes with the jingles,
// and episodes.duration_sec — the RSS <itunes:duration> — counts them.
func TestPipeline_Run_JingleWrapsEveryEpisode(t *testing.T) {
	for _, shape := range episodeShapes() {
		t.Run(shape.name, func(t *testing.T) {
			d := defaultDeps()
			p := shape.setup(t, d)

			require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

			require.Len(t, d.encoder.calls, len(shape.kinds))
			for i, call := range d.encoder.calls {
				assertJingleWrapped(t, call.wavPaths)
				ep, _ := d.episodes.byKind(shape.kinds[i])
				require.NotNil(t, ep)
				assert.Equal(t, shape.speechSec[i]+jingleSec, ep.DurationSec,
					"%s の duration_sec に ジングル 22s が乗る", shape.kinds[i])
			}
		})
	}
}

// TestPipeline_Run_JingleDecodeFailureDegrades pins §8: ジングルは番組の装飾
// であり、その欠落は放送を止める理由にならない。The episode ships without
// them and its recorded duration counts only the audio that exists.
func TestPipeline_Run_JingleDecodeFailureDegrades(t *testing.T) {
	for _, shape := range episodeShapes() {
		t.Run(shape.name, func(t *testing.T) {
			d := defaultDeps()
			d.encoder.jingleErr = errors.New("exec: \"ffmpeg\": executable file not found in $PATH")
			p := shape.setup(t, d)
			logs := captureLogs(p)

			require.NoError(t, p.Run(context.Background(), radio.RunOptions{}),
				"ジングル欠落でエピソード生成全体を失敗させない (§8)")

			require.Len(t, d.encoder.calls, len(shape.kinds))
			for i, call := range d.encoder.calls {
				assertNoJingle(t, call.wavPaths)
				ep, _ := d.episodes.byKind(shape.kinds[i])
				require.NotNil(t, ep, "%s は出荷される", shape.kinds[i])
				assert.Equal(t, shape.speechSec[i], ep.DurationSec,
					"欠けたジングルの尺は加算しない — duration_sec は実 mp3 と一致し続ける")
			}
			assert.Contains(t, logs.String(), "jingle decode failed")
			assert.Contains(t, logs.String(), "executable file not found",
				"原因が Warn ログに残る")
		})
	}
}

// TestPipeline_Run_JingleDecodedOncePerRun pins that the twin reuses the
// public run's wavs: one decode, one pair of files, referenced from both
// concat lists (§6-4: 同一 run 内で無駄な再デコードをしない).
func TestPipeline_Run_JingleDecodedOncePerRun(t *testing.T) {
	d := defaultDeps()
	p := learningPipeline(t, d, &fakeLearning{due: sampleDueItems()})

	require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

	require.Len(t, d.encoder.jingleFormats, 1, "1 run につき 1 回のデコード")
	require.Len(t, d.encoder.calls, 2)
	pub := d.encoder.calls[0].wavPaths
	priv := d.encoder.calls[1].wavPaths
	assert.Equal(t, pub[0], priv[0], "同じオープニング wav を参照する")
	assert.Equal(t, pub[len(pub)-1], priv[len(priv)-1], "同じエンディング wav を参照する")
}

// TestPipeline_Run_JingleFormatFollowsEngine pins §12-5: the format asked of
// ffmpeg is the one this run's VOICEVOX output actually has — read from the
// synthesized bytes, never configured. A concat list mixing formats is the
// exact breakage this indirection exists to prevent.
func TestPipeline_Run_JingleFormatFollowsEngine(t *testing.T) {
	d := defaultDeps()

	require.NoError(t, newPipeline(t, d).Run(context.Background(), radio.RunOptions{}))

	require.Len(t, d.encoder.jingleFormats, 1)
	assert.Equal(t,
		tts.WavFormat{AudioFormat: 1, Channels: 1, SampleRate: 24000, BitsPerSample: 16},
		d.encoder.jingleFormats[0],
		"fakeTTS が返す wav (validWav) のフォーマットがそのまま渡る")
}

// TestPipeline_Run_JingleCountsTowardLengthGuard pins the §7.1 book_review
// guard's input: the jingles are part of the episode the listener sits
// through, so they count toward PRIVATE_EPISODE_MAX_MINUTES. The cap here is
// set so that news+quiz+book_review estimate fits with speech alone but not
// once the jingles are added.
func TestPipeline_Run_JingleCountsTowardLengthGuard(t *testing.T) {
	// news 120s + corner 156s + book_review 推定 180s = 456s; ジングル込み 478s.
	const maxLen = 470 * time.Second

	t.Run("ジングルありは超過して翌日回し", func(t *testing.T) {
		d := defaultDeps()
		br := activeBookReview()
		reviewer := &fakeBookReviewer{result: sampleBookResult()}
		p := bookReviewPipeline(t, d, &fakeLearning{due: sampleDueItems()}, br, reviewer)
		p.Config.PrivateEpisodeMax = maxLen

		require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

		assert.Empty(t, reviewer.calls, "尺ガードは生成前に判定する (§7.1)")
		assert.Empty(t, br.advances, "翌日回し: カーソルは動かさない")
	})

	t.Run("ジングルが縮退した run では収まる", func(t *testing.T) {
		d := defaultDeps()
		d.encoder.jingleErr = errors.New("ffmpeg down")
		br := activeBookReview()
		reviewer := &fakeBookReviewer{result: sampleBookResult()}
		p := bookReviewPipeline(t, d, &fakeLearning{due: sampleDueItems()}, br, reviewer)
		p.Config.PrivateEpisodeMax = maxLen

		require.NoError(t, p.Run(context.Background(), radio.RunOptions{}))

		require.Len(t, reviewer.calls, 1,
			"実測値ベースなので、欠けたジングルの分だけガードは自然に緩む")
		require.Len(t, br.advances, 1)
	})
}
