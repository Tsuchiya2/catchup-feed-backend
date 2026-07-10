package radio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/learning"
	"catchup-feed/internal/script"
)

// weeklyReviewPlan is a fully prepared §7.4 週次振り返り ready to inject into
// the private episode: the segment row (kind='review', §12-6 constant via
// entity), its synthesized wavs and length, and the material behind the
// §7.5 show-notes section. Unlike book_review it carries no DB state to
// commit — the material is a read and the script a template, so a same-day
// rev simply regenerates the identical segment (idempotent by construction,
// no cursor/dedupe needed).
type weeklyReviewPlan struct {
	segment  *entity.Segment
	wavs     []string
	duration time.Duration
	material learning.WeeklyReview
}

// weeklyReviewMaterial is the read-only §7.4 gate shared by dry-run preview and
// real generation: it returns the material only when the review is due this
// broadcast (learning enabled, the configured weekday, D-21) AND there is
// something to say (non-empty window). It makes no writes and never fails the
// run — a lookup error degrades to "no review this week" with a warning (§9).
func (p *Pipeline) weeklyReviewMaterial(ctx context.Context, logger *slog.Logger, now time.Time) (learning.WeeklyReview, bool) {
	if p.Learning == nil {
		return learning.WeeklyReview{}, false
	}
	if learning.BroadcastWeekday(now) != p.LearningCfg.WeeklyReviewDOW {
		return learning.WeeklyReview{}, false // 週次振り返りの曜日ではない (§7.4/D-21)
	}
	material, err := p.Learning.WeeklyReviewMaterial(ctx,
		learning.WeeklyReviewWindowStart(now), len(p.LearningCfg.Ladder))
	if err != nil {
		logger.Warn("weekly review skipped: material lookup failed (§9)", slog.Any("error", err))
		return learning.WeeklyReview{}, false
	}
	if material.IsEmpty() {
		// 素材ゼロの週は空の振り返りを作らない (§7.4)。
		return learning.WeeklyReview{}, false
	}
	return material, true
}

// prepareWeeklyReview builds the injectable §7.4 plan: gate → template script
// (LLM 不使用) → TTS into dir. It returns nil when the review is not due, the
// week is empty, or TTS degrades (§9: 振り返りだけ諦め、私的版本体は出す).
// No length guard here — the review is short and weekly; its measured duration
// is instead FED INTO the book_review length guard by the caller (手順6 申し送り).
func (p *Pipeline) prepareWeeklyReview(ctx context.Context, logger *slog.Logger, dir string, now time.Time) *weeklyReviewPlan {
	material, ok := p.weeklyReviewMaterial(ctx, logger, now)
	if !ok {
		return nil
	}
	body, ok := script.BuildWeeklyReview(material)
	if !ok {
		return nil // IsEmpty already filtered; defensive
	}

	audios, err := p.TTS.SynthesizeScript(ctx, body)
	if err != nil {
		logger.Warn("weekly review skipped: TTS failed (§9)", slog.Any("error", err))
		return nil
	}
	var wavs []string
	var total time.Duration
	for j, audio := range audios {
		path := filepath.Join(dir, fmt.Sprintf("weeklyreview_%03d.wav", j))
		if err := os.WriteFile(path, audio.Data, 0o600); err != nil {
			logger.Warn("weekly review skipped: write wav failed (§9)", slog.Any("error", err))
			return nil
		}
		wavs = append(wavs, path)
		total += audio.Duration
	}

	logger.Info("weekly review segment prepared (§7.4)",
		slog.Int("concepts", len(material.Concepts)),
		slog.Int("graduated", material.GraduatedCount),
		slog.Bool("reintroduced", material.Reintroduced != ""))

	return &weeklyReviewPlan{
		segment:  &entity.Segment{Kind: entity.SegmentKindReview, Script: body},
		wavs:     wavs,
		duration: total,
		material: material,
	}
}

// weeklyReviewDuration is the plan's audio length, or zero when no review runs.
func weeklyReviewDuration(plan *weeklyReviewPlan) time.Duration {
	if plan == nil {
		return 0
	}
	return plan.duration
}

// previewWeeklyReview is the dry-run read-only peek at this week's material:
// it returns a pointer to the material when a review is due and non-empty,
// else nil. No TTS, no writes (D-2: プロンプト・尺調整用の目視).
func (p *Pipeline) previewWeeklyReview(ctx context.Context, logger *slog.Logger, now time.Time) *learning.WeeklyReview {
	material, ok := p.weeklyReviewMaterial(ctx, logger, now)
	if !ok {
		return nil
	}
	return &material
}
