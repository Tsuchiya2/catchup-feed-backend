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

	t.Run("delivers the failure notice to every destination (D-29: email)", func(t *testing.T) {
		payload, err := jobs.NewNotifyErrorPayload("radio", "VOICEVOX unreachable")
		require.NoError(t, err)

		email := &fakeDestination{name: "email"}
		handler := &jobs.NotifyErrorHandler{
			Destinations: []notify.Destination{email},
			Logger:       slog.New(slog.DiscardHandler),
		}
		require.NoError(t, handler.Handle(context.Background(), newJob(string(payload))))

		require.Len(t, email.got, 1)
		assert.Contains(t, email.got[0].Subject, "radio")
		assert.Equal(t, "VOICEVOX unreachable", email.got[0].Body)
	})

	t.Run("delivery failure is swallowed — best-effort, never retried (§8)", func(t *testing.T) {
		broken := &fakeDestination{name: "email", err: errors.New("smtp down")}
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
