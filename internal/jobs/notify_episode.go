package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/notify"
)

// Mailer is the slice of notify.SMTPMailer the episode handler needs —
// one plain-text mail to one friend (C-11).
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// EpisodeGetter is the slice of repository.EpisodeRepository this handler
// needs.
type EpisodeGetter interface {
	Get(ctx context.Context, id int64) (*entity.Episode, error)
}

// SubscriberLister is the slice of repository.SubscriberRepository this
// handler needs.
type SubscriberLister interface {
	List(ctx context.Context) ([]*entity.Subscriber, error)
}

// notifyEpisodePayload is the §6-5 contract with the radio batch.
type notifyEpisodePayload struct {
	EpisodeID int64 `json:"episode_id"`
}

// NotifyEpisodeHandler handles 'notify_episode' (§7): the admin channels
// (Destinations, D-7) get title + show notes + episode URL, with the mp3
// attached on Discord for small public episodes; active friends with an
// email address get a plain-text new-episode mail (C-11) — public episodes
// only, private ones live outside the subscriber concept (C-5).
type NotifyEpisodeHandler struct {
	Episodes     EpisodeGetter
	Subscribers  SubscriberLister
	Destinations []notify.Destination
	// Mailer sends friend mail; nil = email channel disabled.
	Mailer Mailer
	// PrivateBaseURL builds the admin-facing episode link
	// ({base}/private/episodes/{id}.mp3). Empty = no link: a public link
	// cannot exist because every public URL embeds a friend's token (C-9)
	// and tokens are unrecoverable hashes (D-5).
	PrivateBaseURL string
	// AudioDir is the episodes directory (same value cleanup and the feed
	// server use). The mp3 is offered for attachment only when
	// episodes.audio_path resolves inside it — the same traversal guard
	// applied everywhere a DB path touches the filesystem.
	AudioDir string
	Logger   *slog.Logger
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
	if episode.FeedKind == entity.FeedKindPublic {
		// §7: Discord attaches the mp3 when it is small enough — public
		// episodes only; the destination enforces the size limit. A path
		// that escapes AudioDir is never handed out; the notification
		// degrades to text-only (§8), the audio stays reachable via the feed.
		if rel, ok := relInsideDir(h.AudioDir, episode.AudioPath); ok {
			msg.AttachmentPath = filepath.Join(h.AudioDir, rel)
			msg.AttachmentBytes = episode.AudioBytes
		} else {
			logger.Warn("notify_episode: audio path outside audio dir, notifying without attachment",
				slog.Int64("episode_id", episode.ID), slog.String("audio_path", episode.AudioPath))
		}
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

	if episode.FeedKind == entity.FeedKindPublic && h.Mailer != nil {
		errs = append(errs, h.mailFriends(ctx, logger, episode)...)
	}
	return errors.Join(errs...)
}

// mailFriends sends the new-episode mail to every active subscriber with
// an email address. The body carries the show notes but no feed URL: the
// subscription URL embeds the friend's token, which only exists as a hash
// (D-5) — the episode reaches them through their podcast app.
func (h *NotifyEpisodeHandler) mailFriends(ctx context.Context, logger *slog.Logger, episode *entity.Episode) []error {
	subscribers, err := h.Subscribers.List(ctx)
	if err != nil {
		return []error{fmt.Errorf("notify_episode: list subscribers: %w", err)}
	}
	body := episode.ShowNotes + "\n\n---\nポッドキャストアプリに新しいエピソードが届いています。"

	var errs []error
	for _, subscriber := range subscribers {
		if !subscriber.IsActive() || subscriber.Email == nil || *subscriber.Email == "" {
			continue
		}
		if err := h.Mailer.Send(ctx, *subscriber.Email, episode.Title, body); err != nil {
			errs = append(errs, fmt.Errorf("notify_episode: email subscriber %d: %w", subscriber.ID, err))
			continue
		}
		logger.Info("jobs: episode mail sent",
			slog.Int64("subscriber_id", subscriber.ID), slog.Int64("episode_id", episode.ID))
	}
	return errs
}

func (h *NotifyEpisodeHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}
