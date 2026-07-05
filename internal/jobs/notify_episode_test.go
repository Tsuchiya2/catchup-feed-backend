package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/jobs"
	"catchup-feed/internal/notify"
)

type fakeDestination struct {
	mu   sync.Mutex
	name string
	err  error
	got  []notify.Message
}

func (d *fakeDestination) Name() string { return d.name }

func (d *fakeDestination) Notify(_ context.Context, msg notify.Message) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return d.err
	}
	d.got = append(d.got, msg)
	return nil
}

type fakeEpisodeGetter struct {
	episodes map[int64]*entity.Episode
	err      error
}

func (g *fakeEpisodeGetter) Get(_ context.Context, id int64) (*entity.Episode, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.episodes[id], nil
}

type fakeSubscriberLister struct {
	subscribers []*entity.Subscriber
	err         error
}

func (l *fakeSubscriberLister) List(_ context.Context) ([]*entity.Subscriber, error) {
	return l.subscribers, l.err
}

type sentMail struct{ to, subject, body string }

type fakeMailer struct {
	mu   sync.Mutex
	err  error
	sent []sentMail
}

func (m *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, sentMail{to: to, subject: subject, body: body})
	return nil
}

func strPtr(s string) *string { return &s }

func episodeJob(id int64) *entity.Job {
	payload, _ := json.Marshal(map[string]int64{"episode_id": id})
	return &entity.Job{ID: 1, Kind: entity.JobKindNotifyEpisode, Payload: payload, Attempts: 1}
}

func testSubscribers() []*entity.Subscriber {
	deactivated := &entity.Subscriber{ID: 3, Name: "ex-friend", Email: strPtr("ex@example.com")}
	t := deactivated.CreatedAt
	deactivated.DeactivatedAt = &t
	return []*entity.Subscriber{
		{ID: 1, Name: "friend-with-mail", Email: strPtr("friend@example.com")},
		{ID: 2, Name: "friend-no-mail"},
		deactivated,
	}
}

func TestNotifyEpisodeHandler_Handle(t *testing.T) {
	publicEpisode := &entity.Episode{
		ID: 7, FeedKind: entity.FeedKindPublic, Title: "pulse 2026-07-05",
		ShowNotes: "notes", AudioPath: "/data/episodes/2026-07-05.mp3", AudioBytes: 5 << 20,
	}
	privateEpisode := &entity.Episode{
		ID: 8, FeedKind: entity.FeedKindPrivate, Title: "private ep",
		ShowNotes: "secret notes", AudioPath: "/data/episodes/p.mp3", AudioBytes: 1024,
	}

	t.Run("public episode notifies destinations and mails active friends (§7)", func(t *testing.T) {
		destination := &fakeDestination{name: "discord"}
		mailer := &fakeMailer{}
		handler := &jobs.NotifyEpisodeHandler{
			Episodes:       &fakeEpisodeGetter{episodes: map[int64]*entity.Episode{7: publicEpisode}},
			Subscribers:    &fakeSubscriberLister{subscribers: testSubscribers()},
			Destinations:   []notify.Destination{destination},
			Mailer:         mailer,
			PrivateBaseURL: "http://pi.tailnet:8081",
			Logger:         slog.New(slog.DiscardHandler),
		}
		require.NoError(t, handler.Handle(context.Background(), episodeJob(7)))

		require.Len(t, destination.got, 1)
		msg := destination.got[0]
		assert.Equal(t, "pulse 2026-07-05", msg.Subject)
		assert.Equal(t, "notes", msg.Body)
		assert.Equal(t, "http://pi.tailnet:8081/private/episodes/7.mp3", msg.Link)
		assert.Equal(t, publicEpisode.AudioPath, msg.AttachmentPath, "public episodes offer the mp3 for Discord attachment")
		assert.Equal(t, publicEpisode.AudioBytes, msg.AttachmentBytes)

		require.Len(t, mailer.sent, 1, "only the active friend with an email is mailed")
		assert.Equal(t, "friend@example.com", mailer.sent[0].to)
		assert.Equal(t, "pulse 2026-07-05", mailer.sent[0].subject)
		assert.Contains(t, mailer.sent[0].body, "notes")
	})

	t.Run("private episode: no friend mail, no attachment (C-5)", func(t *testing.T) {
		destination := &fakeDestination{name: "discord"}
		mailer := &fakeMailer{}
		handler := &jobs.NotifyEpisodeHandler{
			Episodes:     &fakeEpisodeGetter{episodes: map[int64]*entity.Episode{8: privateEpisode}},
			Subscribers:  &fakeSubscriberLister{subscribers: testSubscribers()},
			Destinations: []notify.Destination{destination},
			Mailer:       mailer,
			Logger:       slog.New(slog.DiscardHandler),
		}
		require.NoError(t, handler.Handle(context.Background(), episodeJob(8)))

		require.Len(t, destination.got, 1)
		assert.Empty(t, destination.got[0].AttachmentPath)
		assert.Empty(t, destination.got[0].Link, "no PrivateBaseURL configured, no link")
		assert.Empty(t, mailer.sent)
	})

	t.Run("nil mailer disables the email channel", func(t *testing.T) {
		handler := &jobs.NotifyEpisodeHandler{
			Episodes:    &fakeEpisodeGetter{episodes: map[int64]*entity.Episode{7: publicEpisode}},
			Subscribers: &fakeSubscriberLister{subscribers: testSubscribers()},
			Logger:      slog.New(slog.DiscardHandler),
		}
		require.NoError(t, handler.Handle(context.Background(), episodeJob(7)))
	})

	t.Run("destination failure is returned for a queue retry, others still delivered", func(t *testing.T) {
		broken := &fakeDestination{name: "discord", err: errors.New("webhook down")}
		working := &fakeDestination{name: "slack"}
		handler := &jobs.NotifyEpisodeHandler{
			Episodes:     &fakeEpisodeGetter{episodes: map[int64]*entity.Episode{7: publicEpisode}},
			Subscribers:  &fakeSubscriberLister{},
			Destinations: []notify.Destination{broken, working},
			Logger:       slog.New(slog.DiscardHandler),
		}
		err := handler.Handle(context.Background(), episodeJob(7))
		require.Error(t, err)
		assert.False(t, jobs.IsPermanent(err), "delivery failures must retry (§7)")
		assert.Len(t, working.got, 1)
	})

	t.Run("mail failure for one friend does not stop the others", func(t *testing.T) {
		mailer := &fakeMailer{err: errors.New("smtp down")}
		handler := &jobs.NotifyEpisodeHandler{
			Episodes:    &fakeEpisodeGetter{episodes: map[int64]*entity.Episode{7: publicEpisode}},
			Subscribers: &fakeSubscriberLister{subscribers: testSubscribers()},
			Mailer:      mailer,
			Logger:      slog.New(slog.DiscardHandler),
		}
		err := handler.Handle(context.Background(), episodeJob(7))
		require.Error(t, err)
		assert.False(t, jobs.IsPermanent(err))
	})

	t.Run("missing episode is permanent (no retry)", func(t *testing.T) {
		handler := &jobs.NotifyEpisodeHandler{
			Episodes:    &fakeEpisodeGetter{episodes: map[int64]*entity.Episode{}},
			Subscribers: &fakeSubscriberLister{},
			Logger:      slog.New(slog.DiscardHandler),
		}
		err := handler.Handle(context.Background(), episodeJob(999))
		require.Error(t, err)
		assert.True(t, jobs.IsPermanent(err))
	})

	t.Run("malformed payload is permanent", func(t *testing.T) {
		handler := &jobs.NotifyEpisodeHandler{
			Episodes:    &fakeEpisodeGetter{},
			Subscribers: &fakeSubscriberLister{},
			Logger:      slog.New(slog.DiscardHandler),
		}
		for _, payload := range []string{`not json`, `{}`, `{"episode_id":0}`} {
			err := handler.Handle(context.Background(), &entity.Job{ID: 1, Payload: json.RawMessage(payload)})
			require.Error(t, err, "payload: %s", payload)
			assert.True(t, jobs.IsPermanent(err), "payload: %s", payload)
		}
	})

	t.Run("episode lookup error retries", func(t *testing.T) {
		handler := &jobs.NotifyEpisodeHandler{
			Episodes:    &fakeEpisodeGetter{err: errors.New("db down")},
			Subscribers: &fakeSubscriberLister{},
			Logger:      slog.New(slog.DiscardHandler),
		}
		err := handler.Handle(context.Background(), episodeJob(7))
		require.Error(t, err)
		assert.False(t, jobs.IsPermanent(err))
	})
}
