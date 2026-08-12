package radio

import (
	"context"
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
	logger.Info("jingles decoded (§6-4)",
		slog.Duration("opening", jingles.Opening.Duration),
		slog.Duration("ending", jingles.Ending.Duration))
	return &jingles
}
