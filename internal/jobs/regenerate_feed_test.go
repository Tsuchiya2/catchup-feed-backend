package jobs_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/jobs"
)

func TestRegenerateFeedHandler_IsANoOp(t *testing.T) {
	// feed.xml is rendered per request; the handler exists as the future
	// cache hook only. It must succeed unconditionally so the queue drains.
	handler := jobs.NewRegenerateFeedHandler(slog.New(slog.DiscardHandler))
	job := &entity.Job{ID: 1, Kind: entity.JobKindRegenerateFeed}
	assert.NoError(t, handler.Handle(context.Background(), job))
}
