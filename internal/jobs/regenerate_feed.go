package jobs

import (
	"context"
	"log/slog"

	"catchup-feed/internal/domain/entity"
)

// NewRegenerateFeedHandler handles 'regenerate_feed' (§3.3: 転送検知 →
// フィード XML 再生成).
//
// Intentionally a no-op today: feed.xml is rendered per request from the
// episodes table (internal/feed), so there is no cached artifact to
// rebuild — by the time the radio batch enqueues this job, the episode row
// is already committed and the next feed request serves it. The job kind
// still exists (and radio keeps enqueuing it) so that introducing a feed
// cache later is a change to this handler only, not to the radio batch or
// the queue contract.
func NewRegenerateFeedHandler(logger *slog.Logger) Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return HandlerFunc(func(_ context.Context, job *entity.Job) error {
		logger.Info("jobs: regenerate_feed is a no-op (feed.xml is rendered per request)",
			slog.Int64("job_id", job.ID))
		return nil
	})
}
