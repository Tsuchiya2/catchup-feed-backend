package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/notify"
)

// EpisodeGetter is the slice of repository.EpisodeRepository this handler
// needs.
type EpisodeGetter interface {
	Get(ctx context.Context, id int64) (*entity.Episode, error)
}

// notifyEpisodePayload is the §6-5 contract with the radio batch.
type notifyEpisodePayload struct {
	EpisodeID int64 `json:"episode_id"`
}

// NotifyEpisodeHandler handles 'notify_episode' (§7): the admin channels
// (Destinations, D-29: email) get title + show notes + episode URL. This
// mail is a delivery-confirmation for the admin only — the friend fan-out
// was removed by D-32 (友人への周知はポッドキャストアプリの購読のみで
// 足りる、C-11 の読み替え).
//
// 契約 (Phase 3 §12-7): radio は notify_episode ジョブを公開エピソードに
// 対して**のみ**積む(「積まない」方式)。このハンドラは feed_kind に依らず
// 管理チャネルへ show_notes を転送するため、私的エピソード — その show
// notes は復習 concept 一覧などの学習コンテンツを含む — のジョブが積まれた
// 時点で §10(学習コンテンツを外部チャネルに流さない)に違反する。
// エンキュー側の遵守が前提であり、ここに feed_kind ガードは足していない
// (公開版の通知経路に一切差分を出さないため、§12-1)。
type NotifyEpisodeHandler struct {
	Episodes     EpisodeGetter
	Destinations []notify.Destination
	// PrivateBaseURL builds the admin-facing episode link
	// ({base}/private/episodes/{id}.mp3). Empty = no link: a public link
	// cannot exist because every public URL embeds a friend's token (C-9)
	// and tokens are unrecoverable hashes (D-5).
	PrivateBaseURL string
	Logger         *slog.Logger
}

// Handle sends the notifications. Failures of individual channels are
// joined and returned so the queue retries (§7, attempts 上限 3); a retry
// re-sends to every channel, so a partially failed fan-out can duplicate a
// message on the channels that already succeeded — accepted for a
// single-user system (§8: 冗長化より縮退許容, the alternative is per-channel
// job bookkeeping).
func (h *NotifyEpisodeHandler) Handle(ctx context.Context, job *entity.Job) error {
	logger := h.logger().With(slog.Int64("job_id", job.ID))

	var payload notifyEpisodePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil || payload.EpisodeID <= 0 {
		return Permanent(fmt.Errorf("notify_episode: invalid payload %q: %w", job.Payload, err))
	}
	episode, err := h.Episodes.Get(ctx, payload.EpisodeID)
	if err != nil {
		return fmt.Errorf("notify_episode: load episode %d: %w", payload.EpisodeID, err)
	}
	if episode == nil {
		return Permanent(fmt.Errorf("notify_episode: episode %d not found", payload.EpisodeID))
	}

	msg := notify.Message{
		Subject: episode.Title,
		Body:    episode.ShowNotes,
	}
	if h.PrivateBaseURL != "" {
		msg.Link = fmt.Sprintf("%s/private/episodes/%d.mp3", h.PrivateBaseURL, episode.ID)
	}

	var errs []error
	for _, destination := range h.Destinations {
		if err := destination.Notify(ctx, msg); err != nil {
			errs = append(errs, fmt.Errorf("notify_episode: %s: %w", destination.Name(), err))
		} else {
			logger.Info("jobs: episode notified",
				slog.String("channel", destination.Name()), slog.Int64("episode_id", episode.ID))
		}
	}

	return errors.Join(errs...)
}

func (h *NotifyEpisodeHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}
