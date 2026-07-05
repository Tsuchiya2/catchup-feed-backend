package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/jobs"
	"catchup-feed/internal/notify"
)

func TestNotifyErrorHandler_Handle(t *testing.T) {
	newJob := func(payload string) *entity.Job {
		return &entity.Job{ID: 42, Kind: entity.JobKindNotifyError, Payload: json.RawMessage(payload)}
	}

	t.Run("delivers the failure notice to every destination", func(t *testing.T) {
		payload, err := jobs.NewNotifyErrorPayload("radio", "VOICEVOX unreachable")
		require.NoError(t, err)

		discord := &fakeDestination{name: "discord"}
		slack := &fakeDestination{name: "slack"}
		handler := &jobs.NotifyErrorHandler{
			Destinations: []notify.Destination{discord, slack},
			Logger:       slog.New(slog.DiscardHandler),
		}
		require.NoError(t, handler.Handle(context.Background(), newJob(string(payload))))

		for _, destination := range []*fakeDestination{discord, slack} {
			require.Len(t, destination.got, 1)
			assert.Contains(t, destination.got[0].Subject, "radio")
			assert.Equal(t, "VOICEVOX unreachable", destination.got[0].Body)
		}
	})

	t.Run("delivery failure is swallowed — best-effort, never retried (§8)", func(t *testing.T) {
		broken := &fakeDestination{name: "discord", err: errors.New("webhook down")}
		handler := &jobs.NotifyErrorHandler{
			Destinations: []notify.Destination{broken},
			Logger:       slog.New(slog.DiscardHandler),
		}
		assert.NoError(t, handler.Handle(context.Background(), newJob(`{"source":"radio","message":"x"}`)))
	})

	t.Run("malformed payload is dropped without error", func(t *testing.T) {
		handler := &jobs.NotifyErrorHandler{Logger: slog.New(slog.DiscardHandler)}
		assert.NoError(t, handler.Handle(context.Background(), newJob(`not json`)))
	})
}
