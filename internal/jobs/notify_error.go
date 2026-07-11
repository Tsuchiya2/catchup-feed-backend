package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/notify"
)

// NotifyErrorPayload is the 'notify_error' contract (§8). Producers (the
// radio batch today) enqueue it best-effort when a run fails.
type NotifyErrorPayload struct {
	// Source names the failed component, e.g. "radio".
	Source string `json:"source"`
	// Message is the failure detail shown to the admin.
	Message string `json:"message"`
}

// NewNotifyErrorPayload marshals the payload for Enqueue.
func NewNotifyErrorPayload(source, message string) (json.RawMessage, error) {
	return json.Marshal(NotifyErrorPayload{Source: source, Message: message})
}

// NotifyErrorHandler handles 'notify_error': the admin-only failure notice
// (§8: VOICEVOX 障害→当日スキップ+通知). Strictly best-effort — it always
// returns nil, so a broken notification channel can never make the failure
// job itself retry or clog the queue (通知の失敗を通知するループを作らない).
// A lost notice is acceptable: the failure is also in the producer's logs,
// and the missing morning episode is its own signal.
type NotifyErrorHandler struct {
	Destinations []notify.Destination
	Logger       *slog.Logger
}

// Handle sends the notice to every admin destination, logging (not
// returning) failures.
func (h *NotifyErrorHandler) Handle(ctx context.Context, job *entity.Job) error {
	logger := h.logger().With(slog.Int64("job_id", job.ID))

	var payload NotifyErrorPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		logger.Error("jobs: notify_error payload invalid, dropping", slog.Any("error", err))
		return nil
	}
	if payload.Source == "" {
		payload.Source = "unknown"
	}
	msg := notify.Message{
		Subject: fmt.Sprintf("catchup-feed 障害: %s の実行が失敗しました", payload.Source),
		Body:    payload.Message,
	}
	for _, destination := range h.Destinations {
		if err := destination.Notify(ctx, msg); err != nil {
			logger.Warn("jobs: error notice delivery failed (best-effort, not retried)",
				slog.String("channel", destination.Name()), slog.Any("error", err))
			continue
		}
		logger.Info("jobs: error notice delivered",
			slog.String("channel", destination.Name()), slog.String("source", payload.Source))
	}
	return nil
}

func (h *NotifyErrorHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}
