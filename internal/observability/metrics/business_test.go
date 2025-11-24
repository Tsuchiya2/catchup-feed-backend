package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRecordArticlesFetched(t *testing.T) {
	tests := []struct {
		name       string
		sourceName string
		sourceID   int64
		count      int
	}{
		{
			name:       "single article",
			sourceName: "Test Source",
			sourceID:   1,
			count:      1,
		},
		{
			name:       "multiple articles",
			sourceName: "Another Source",
			sourceID:   2,
			count:      10,
		},
		{
			name:       "zero articles",
			sourceName: "Empty Source",
			sourceID:   3,
			count:      0,
		},
		{
			name:       "empty source name",
			sourceName: "",
			sourceID:   4,
			count:      5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordArticlesFetched(tt.sourceName, tt.sourceID, tt.count)
			})
		})
	}
}

func TestRecordArticleSummarized(t *testing.T) {
	tests := []struct {
		name    string
		success bool
	}{
		{
			name:    "success",
			success: true,
		},
		{
			name:    "failure",
			success: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordArticleSummarized(tt.success)
			})
		})
	}
}

func TestRecordSummarizationDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
	}{
		{
			name:     "fast response",
			duration: 100 * time.Millisecond,
		},
		{
			name:     "normal response",
			duration: 1 * time.Second,
		},
		{
			name:     "slow response",
			duration: 5 * time.Second,
		},
		{
			name:     "zero duration",
			duration: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordSummarizationDuration(tt.duration)
			})
		})
	}
}

func TestRecordFeedCrawl(t *testing.T) {
	tests := []struct {
		name            string
		sourceID        int64
		duration        time.Duration
		itemsFound      int64
		itemsInserted   int64
		itemsDuplicated int64
	}{
		{
			name:            "successful crawl",
			sourceID:        1,
			duration:        2 * time.Second,
			itemsFound:      10,
			itemsInserted:   8,
			itemsDuplicated: 2,
		},
		{
			name:            "empty crawl",
			sourceID:        2,
			duration:        500 * time.Millisecond,
			itemsFound:      0,
			itemsInserted:   0,
			itemsDuplicated: 0,
		},
		{
			name:            "all duplicates",
			sourceID:        3,
			duration:        1 * time.Second,
			itemsFound:      5,
			itemsInserted:   0,
			itemsDuplicated: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordFeedCrawl(tt.sourceID, tt.duration, tt.itemsFound, tt.itemsInserted, tt.itemsDuplicated)
			})
		})
	}
}

func TestRecordFeedCrawlError(t *testing.T) {
	tests := []struct {
		name      string
		sourceID  int64
		errorType string
	}{
		{
			name:      "fetch failed",
			sourceID:  1,
			errorType: "fetch_failed",
		},
		{
			name:      "parse error",
			sourceID:  2,
			errorType: "parse_error",
		},
		{
			name:      "timeout",
			sourceID:  3,
			errorType: "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordFeedCrawlError(tt.sourceID, tt.errorType)
			})
		})
	}
}

func TestUpdateArticlesTotal(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{
			name:  "zero articles",
			count: 0,
		},
		{
			name:  "some articles",
			count: 100,
		},
		{
			name:  "many articles",
			count: 10000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				UpdateArticlesTotal(tt.count)
			})
		})
	}
}

func TestUpdateSourcesTotal(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{
			name:  "zero sources",
			count: 0,
		},
		{
			name:  "some sources",
			count: 10,
		},
		{
			name:  "many sources",
			count: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				UpdateSourcesTotal(tt.count)
			})
		})
	}
}

func TestRecordDBQuery(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		duration  time.Duration
	}{
		{
			name:      "select query",
			operation: "select_articles",
			duration:  10 * time.Millisecond,
		},
		{
			name:      "insert query",
			operation: "insert_article",
			duration:  5 * time.Millisecond,
		},
		{
			name:      "slow query",
			operation: "complex_join",
			duration:  500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordDBQuery(tt.operation, tt.duration)
			})
		})
	}
}

func TestUpdateDBConnectionStats(t *testing.T) {
	tests := []struct {
		name   string
		active int
		idle   int
	}{
		{
			name:   "no connections",
			active: 0,
			idle:   0,
		},
		{
			name:   "some active",
			active: 5,
			idle:   10,
		},
		{
			name:   "all active",
			active: 25,
			idle:   0,
		},
		{
			name:   "all idle",
			active: 0,
			idle:   25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				UpdateDBConnectionStats(tt.active, tt.idle)
			})
		})
	}
}

func TestMetricsFunctions_AllCallable(t *testing.T) {
	// Test that all functions can be called in sequence without panic
	assert.NotPanics(t, func() {
		RecordArticlesFetched("Test Source", 1, 10)
		RecordArticleSummarized(true)
		RecordSummarizationDuration(1 * time.Second)
		RecordFeedCrawl(1, 2*time.Second, 10, 8, 2)
		RecordFeedCrawlError(1, "test_error")
		UpdateArticlesTotal(100)
		UpdateSourcesTotal(10)
		RecordDBQuery("test_operation", 10*time.Millisecond)
		UpdateDBConnectionStats(5, 10)
	})
}
