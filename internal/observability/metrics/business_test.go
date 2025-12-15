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

func TestRecordContentFetchSuccess(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		size     int
	}{
		{
			name:     "small content",
			duration: 100 * time.Millisecond,
			size:     1024, // 1KB
		},
		{
			name:     "medium content",
			duration: 500 * time.Millisecond,
			size:     102400, // 100KB
		},
		{
			name:     "large content",
			duration: 2 * time.Second,
			size:     1048576, // 1MB
		},
		{
			name:     "very large content",
			duration: 5 * time.Second,
			size:     10485760, // 10MB
		},
		{
			name:     "zero size",
			duration: 50 * time.Millisecond,
			size:     0,
		},
		{
			name:     "fast fetch",
			duration: 10 * time.Millisecond,
			size:     500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordContentFetchSuccess(tt.duration, tt.size)
			})
		})
	}
}

func TestRecordContentFetchFailed(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
	}{
		{
			name:     "quick failure",
			duration: 100 * time.Millisecond,
		},
		{
			name:     "timeout failure",
			duration: 10 * time.Second,
		},
		{
			name:     "immediate failure",
			duration: 0,
		},
		{
			name:     "slow failure",
			duration: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordContentFetchFailed(tt.duration)
			})
		})
	}
}

func TestRecordContentFetchSkipped(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "single skip"},
		{name: "multiple skips"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordContentFetchSkipped()
			})
		})
	}
}

func TestRecordHTTPRequest(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		status       string
		duration     time.Duration
		requestSize  int
		responseSize int
	}{
		{
			name:         "GET request success",
			method:       "GET",
			path:         "/api/articles",
			status:       "200",
			duration:     50 * time.Millisecond,
			requestSize:  0,
			responseSize: 5000,
		},
		{
			name:         "POST request success",
			method:       "POST",
			path:         "/api/sources",
			status:       "201",
			duration:     100 * time.Millisecond,
			requestSize:  1500,
			responseSize: 500,
		},
		{
			name:         "DELETE request",
			method:       "DELETE",
			path:         "/api/articles/123",
			status:       "204",
			duration:     30 * time.Millisecond,
			requestSize:  0,
			responseSize: 0,
		},
		{
			name:         "error response",
			method:       "GET",
			path:         "/api/notfound",
			status:       "404",
			duration:     20 * time.Millisecond,
			requestSize:  0,
			responseSize: 200,
		},
		{
			name:         "server error",
			method:       "POST",
			path:         "/api/error",
			status:       "500",
			duration:     500 * time.Millisecond,
			requestSize:  2000,
			responseSize: 100,
		},
		{
			name:         "large request and response",
			method:       "POST",
			path:         "/api/upload",
			status:       "200",
			duration:     2 * time.Second,
			requestSize:  1048576,  // 1MB
			responseSize: 1048576,  // 1MB
		},
		{
			name:         "zero sizes",
			method:       "HEAD",
			path:         "/api/health",
			status:       "200",
			duration:     5 * time.Millisecond,
			requestSize:  0,
			responseSize: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordHTTPRequest(tt.method, tt.path, tt.status, tt.duration, tt.requestSize, tt.responseSize)
			})
		})
	}
}

func TestRecordOperationDuration(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		duration  time.Duration
	}{
		{
			name:      "fast operation",
			operation: "cache_lookup",
			duration:  1 * time.Millisecond,
		},
		{
			name:      "normal operation",
			operation: "api_call",
			duration:  100 * time.Millisecond,
		},
		{
			name:      "slow operation",
			operation: "batch_process",
			duration:  5 * time.Second,
		},
		{
			name:      "zero duration",
			operation: "instant_operation",
			duration:  0,
		},
		{
			name:      "very long operation",
			operation: "migration",
			duration:  1 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordOperationDuration(tt.operation, tt.duration)
			})
		})
	}
}

func TestRecordArticleSummarized_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		success bool
		count   int // number of times to call
	}{
		{
			name:    "multiple successes",
			success: true,
			count:   10,
		},
		{
			name:    "multiple failures",
			success: false,
			count:   5,
		},
		{
			name:    "single call",
			success: true,
			count:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				for i := 0; i < tt.count; i++ {
					RecordArticleSummarized(tt.success)
				}
			})
		})
	}
}

func TestRecordSummarizationDuration_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
	}{
		{
			name:     "very long duration",
			duration: 1 * time.Hour,
		},
		{
			name:     "extremely fast",
			duration: 1 * time.Microsecond,
		},
		{
			name:     "negative duration",
			duration: -1 * time.Second,
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

func TestRecordArticlesFetched_Concurrent(t *testing.T) {
	// Test concurrent calls to ensure thread-safety
	t.Run("concurrent calls", func(t *testing.T) {
		done := make(chan bool)

		for i := 0; i < 10; i++ {
			go func(id int) {
				defer func() { done <- true }()
				RecordArticlesFetched("Test Source", int64(id), 1)
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}
	})
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
		RecordContentFetchSuccess(100*time.Millisecond, 1024)
		RecordContentFetchFailed(200 * time.Millisecond)
		RecordContentFetchSkipped()
		RecordHTTPRequest("GET", "/api/test", "200", 50*time.Millisecond, 0, 500)
		RecordOperationDuration("test_op", 100*time.Millisecond)
	})
}
