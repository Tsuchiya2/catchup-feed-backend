package script_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/repository"
	"catchup-feed/internal/script"
)

func day(d int) time.Time {
	return time.Date(2026, 7, d, 12, 0, 0, 0, time.UTC)
}

func TestPlan(t *testing.T) {
	articles := []repository.RadioArticle{
		{ID: 1, Title: "go old", Category: "golang", PublishedAt: day(1)},
		{ID: 2, Title: "ai old", Category: "ai", PublishedAt: day(2)},
		{ID: 3, Title: "go new", Category: "golang", PublishedAt: day(3)},
		{ID: 4, Title: "ai new", Category: "ai", PublishedAt: day(4)},
		{ID: 5, Title: "infra", Category: "infra", PublishedAt: day(5)},
	}

	tests := []struct {
		name         string
		articles     []repository.RadioArticle
		maxFeatured  int
		wantFeatured []int64 // article IDs, on-air order
		wantOverflow []int64 // article IDs, show-notes order
	}{
		{
			name:         "under the cap keeps everything, grouped by category",
			articles:     articles,
			maxFeatured:  8,
			wantFeatured: []int64{2, 4, 1, 3, 5}, // ai(old->new), golang(old->new), infra
			wantOverflow: nil,
		},
		{
			name:         "over the cap drops the oldest to the show notes",
			articles:     articles,
			maxFeatured:  3,
			wantFeatured: []int64{4, 3, 5}, // newest 3 = {5,4,3} -> ai, golang, infra
			wantOverflow: []int64{2, 1},    // newest first
		},
		{
			name:         "cap of zero puts everything in the show notes",
			articles:     articles[:2],
			maxFeatured:  0,
			wantFeatured: nil,
			wantOverflow: []int64{2, 1},
		},
		{
			name:         "empty input",
			articles:     nil,
			maxFeatured:  8,
			wantFeatured: nil,
			wantOverflow: nil,
		},
		{
			name: "same timestamp breaks ties by id",
			articles: []repository.RadioArticle{
				{ID: 7, Title: "b", Category: "x", PublishedAt: day(1)},
				{ID: 6, Title: "a", Category: "x", PublishedAt: day(1)},
			},
			maxFeatured:  1,
			wantFeatured: []int64{7},
			wantOverflow: []int64{6},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featured, overflow := script.Plan(tt.articles, tt.maxFeatured)

			assert.Equal(t, tt.wantFeatured, ids(featured), "featured order")
			assert.Equal(t, tt.wantOverflow, ids(overflow), "overflow order")
		})
	}
}

func TestPlan_DoesNotMutateInput(t *testing.T) {
	articles := []repository.RadioArticle{
		{ID: 1, Category: "b", PublishedAt: day(1)},
		{ID: 2, Category: "a", PublishedAt: day(2)},
	}
	script.Plan(articles, 8)
	require.Equal(t, int64(1), articles[0].ID, "input slice must not be reordered")
}

func ids(articles []repository.RadioArticle) []int64 {
	if len(articles) == 0 {
		return nil
	}
	out := make([]int64, len(articles))
	for i, a := range articles {
		out[i] = a.ID
	}
	return out
}
