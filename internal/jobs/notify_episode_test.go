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

func episodeJob(id int64) *entity.Job {
	payload, _ := json.Marshal(map[string]int64{"episode_id": id})
	return &entity.Job{ID: 1, Kind: entity.JobKindNotifyEpisode, Payload: payload, Attempts: 1}
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

	// Success cases: admin destinations only, regardless of feed_kind
	// (§7 / D-32: no friend mail).
	successCases := []struct {
		name           string
		episode        *entity.Episode
		privateBaseURL string
		hasDestination bool
		wantSubject    string
		wantBody       string
		wantLink       string
	}{
		{
			name:           "public episode notifies admin destinations (§7 / D-32: no friend mail)",
			episode:        publicEpisode,
			privateBaseURL: "http://pi.tailnet:8081",
			hasDestination: true,
			wantSubject:    "pulse 2026-07-05",
			wantBody:       "notes",
			wantLink:       "http://pi.tailnet:8081/private/episodes/7.mp3",
		},
		{
			name:           "private episode also notifies admin destinations (feed_kind に依らず)",
			episode:        privateEpisode,
			hasDestination: true,
			wantSubject:    "private ep",
			wantBody:       "secret notes",
			wantLink:       "", // no PrivateBaseURL configured, no link
		},
		{
			name:           "no destinations configured is a no-op success",
			episode:        publicEpisode,
			hasDestination: false,
		},
	}
	for _, tc := range successCases {
		t.Run(tc.name, func(t *testing.T) {
			destination := &fakeDestination{name: "email"}
			handler := &jobs.NotifyEpisodeHandler{
				Episodes:       &fakeEpisodeGetter{episodes: map[int64]*entity.Episode{tc.episode.ID: tc.episode}},
				PrivateBaseURL: tc.privateBaseURL,
				Logger:         slog.New(slog.DiscardHandler),
			}
			if tc.hasDestination {
				handler.Destinations = []notify.Destination{destination}
			}
			require.NoError(t, handler.Handle(context.Background(), episodeJob(tc.episode.ID)))

			if !tc.hasDestination {
				assert.Empty(t, destination.got)
				return
			}
			require.Len(t, destination.got, 1)
			msg := destination.got[0]
			assert.Equal(t, tc.wantSubject, msg.Subject)
			assert.Equal(t, tc.wantBody, msg.Body)
			assert.Equal(t, tc.wantLink, msg.Link)
		})
	}

	t.Run("destination failure is returned for a queue retry, others still delivered", func(t *testing.T) {
		broken := &fakeDestination{name: "email", err: errors.New("smtp down")}
		working := &fakeDestination{name: "second"}
		handler := &jobs.NotifyEpisodeHandler{
			Episodes:     &fakeEpisodeGetter{episodes: map[int64]*entity.Episode{7: publicEpisode}},
			Destinations: []notify.Destination{broken, working},
			Logger:       slog.New(slog.DiscardHandler),
		}
		err := handler.Handle(context.Background(), episodeJob(7))
		require.Error(t, err)
		assert.False(t, jobs.IsPermanent(err), "delivery failures must retry (§7)")
		assert.Len(t, working.got, 1)
	})

	t.Run("missing episode is permanent (no retry)", func(t *testing.T) {
		handler := &jobs.NotifyEpisodeHandler{
			Episodes: &fakeEpisodeGetter{episodes: map[int64]*entity.Episode{}},
			Logger:   slog.New(slog.DiscardHandler),
		}
		err := handler.Handle(context.Background(), episodeJob(999))
		require.Error(t, err)
		assert.True(t, jobs.IsPermanent(err))
	})

	t.Run("malformed payload is permanent", func(t *testing.T) {
		handler := &jobs.NotifyEpisodeHandler{
			Episodes: &fakeEpisodeGetter{},
			Logger:   slog.New(slog.DiscardHandler),
		}
		for _, payload := range []string{`not json`, `{}`, `{"episode_id":0}`} {
			err := handler.Handle(context.Background(), &entity.Job{ID: 1, Payload: json.RawMessage(payload)})
			require.Error(t, err, "payload: %s", payload)
			assert.True(t, jobs.IsPermanent(err), "payload: %s", payload)
		}
	})

	t.Run("episode lookup error retries", func(t *testing.T) {
		handler := &jobs.NotifyEpisodeHandler{
			Episodes: &fakeEpisodeGetter{err: errors.New("db down")},
			Logger:   slog.New(slog.DiscardHandler),
		}
		err := handler.Handle(context.Background(), episodeJob(7))
		require.Error(t, err)
		assert.False(t, jobs.IsPermanent(err))
	})
}
