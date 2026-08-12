package radio

import (
	"context"
	"fmt"
	"log/slog"

	"catchup-feed/internal/tts"
)

// prepareJingles decodes the run's opening/ending jingles into dir, in the
// exact wav format this run's VOICEVOX output has (§6-4 結合 / §12-5: concat
// に混ぜる wav はフォーマットが一致していなければならず、その format は設定
// 値ではなく実際のエンジン出力から導く).
//
// It is called once per run and the result is shared by the public and the
// private episode — same temp dir, same wavs, no second decode.
//
// Every failure (ffmpeg 不在・mp3 破損・フォーマット不一致) returns nil with a
// warning instead of an error: ジングルは番組の装飾であり、その欠落は放送を
// 止める理由にならない (§8 縮退許容). *tts.Jingles is nil-safe on both Wrap
// and Duration, so the concat list and episodes.duration_sec fall back to
// speech-only without any branching at the call sites.
func (p *Pipeline) prepareJingles(ctx context.Context, logger *slog.Logger, dir string, format tts.WavFormat) *tts.Jingles {
	jingles, err := p.Encoder.DecodeJingles(ctx, dir, format)
	if err != nil {
		logger.Warn("jingle decode failed, shipping the episode without jingles (§8 縮退)",
			slog.Any("error", err))
		return nil
	}
	// Distinct message on purpose: here the decode SUCCEEDED and the result
	// was refused. Reporting it as a decode failure would send whoever reads
	// the log at ffmpeg instead of at the Encoder implementation.
	if err := validJinglePair(jingles); err != nil {
		logger.Warn("jingle decode returned an unusable pair, shipping the episode without jingles (§8 縮退)",
			slog.Any("error", err))
		return nil
	}
	logger.Info("jingles decoded (§6-4)",
		slog.Duration("opening", jingles.Opening.Duration),
		slog.Duration("ending", jingles.Ending.Duration))
	return &jingles
}

// validJinglePair rejects a pair that would poison what the run produces.
//
// Encoder is an interface, so "DecodeJingles returned no error" is a promise
// from an implementation, not a fact about the values: a zero-value
// tts.Jingles handed back with a nil error would write a concat-list entry
// with an empty filename and add zero seconds to episodes.duration_sec. tts.FFmpeg
// never does that — it errors on every path that could — but this is the
// boundary where the pipeline stops trusting and starts checking, because
// being wrong here costs a broken or mislabelled episode. A rejected pair
// takes the same §8 route as a failed decode: warn, ship without jingles.
func validJinglePair(j tts.Jingles) error {
	for _, part := range []struct {
		name   string
		jingle tts.Jingle
	}{
		{"opening", j.Opening},
		{"ending", j.Ending},
	} {
		if part.jingle.Path == "" {
			return fmt.Errorf("radio: %s jingle has no wav path", part.name)
		}
		if part.jingle.Duration <= 0 {
			return fmt.Errorf("radio: %s jingle has non-positive duration %s",
				part.name, part.jingle.Duration)
		}
	}
	return nil
}
