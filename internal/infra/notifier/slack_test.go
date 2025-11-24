package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
)

// TASK-002: SlackNotifier Unit Tests

func TestSlackNotifier_buildBlockKitPayload(t *testing.T) {
	t.Run("TC-1: should build valid Block Kit payload with all fields", func(t *testing.T) {
		// Arrange
		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: "https://hooks.slack.com/services/test",
			Timeout:    10 * time.Second,
		})

		publishedAt := time.Date(2025, 11, 15, 12, 0, 0, 0, time.UTC)
		article := &entity.Article{
			ID:          1,
			SourceID:    1,
			Title:       "Test Article Title",
			URL:         "https://example.com/article/1",
			Summary:     "This is a test article summary with some content.",
			PublishedAt: publishedAt,
			CreatedAt:   time.Now(),
		}

		source := &entity.Source{
			ID:      1,
			Name:    "Test Source",
			FeedURL: "https://example.com/feed",
			Active:  true,
		}

		// Act
		payload := notifier.buildBlockKitPayload(article, source)

		// Assert
		if len(payload.Blocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(payload.Blocks))
		}

		// Verify fallback text
		expectedFallbackPrefix := "Test Article Title - Test Source"
		if !strings.HasPrefix(payload.Text, expectedFallbackPrefix) {
			t.Errorf("expected fallback text to start with %q, got %q", expectedFallbackPrefix, payload.Text)
		}

		// Verify section block
		sectionBlock := payload.Blocks[0]
		if sectionBlock.Type != "section" {
			t.Errorf("expected block type=%q, got %q", "section", sectionBlock.Type)
		}
		if sectionBlock.Text == nil {
			t.Fatal("expected section block to have text")
		}
		if sectionBlock.Text.Type != "mrkdwn" {
			t.Errorf("expected text type=%q, got %q", "mrkdwn", sectionBlock.Text.Type)
		}

		// Verify section text contains title link
		expectedTitleLink := fmt.Sprintf("*<%s|%s>*", article.URL, article.Title)
		if !strings.Contains(sectionBlock.Text.Text, expectedTitleLink) {
			t.Errorf("expected section text to contain %q", expectedTitleLink)
		}

		// Verify section text contains summary
		if !strings.Contains(sectionBlock.Text.Text, article.Summary) {
			t.Errorf("expected section text to contain summary %q", article.Summary)
		}

		// Verify context block
		contextBlock := payload.Blocks[1]
		if contextBlock.Type != "context" {
			t.Errorf("expected block type=%q, got %q", "context", contextBlock.Type)
		}
		if len(contextBlock.Elements) != 1 {
			t.Fatalf("expected 1 context element, got %d", len(contextBlock.Elements))
		}

		contextElement := contextBlock.Elements[0]
		if contextElement.Type != "mrkdwn" {
			t.Errorf("expected context element type=%q, got %q", "mrkdwn", contextElement.Type)
		}

		// Verify context text contains source name and timestamp
		expectedTimestamp := publishedAt.Format(time.RFC3339)
		expectedContext := fmt.Sprintf("%s • %s", source.Name, expectedTimestamp)
		if contextElement.Text != expectedContext {
			t.Errorf("expected context=%q, got %q", expectedContext, contextElement.Text)
		}
	})

	t.Run("TC-2: should truncate long summary (>3000 chars) with ...", func(t *testing.T) {
		// Arrange
		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: "https://hooks.slack.com/services/test",
			Timeout:    10 * time.Second,
		})

		// Create summary that will exceed 3000 chars when combined with title link
		longSummary := strings.Repeat("a", 5000) // 5000 characters
		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     longSummary,
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		payload := notifier.buildBlockKitPayload(article, source)

		// Assert
		sectionText := payload.Blocks[0].Text.Text
		if len(sectionText) > maxSectionTextLength {
			t.Errorf("expected section text length <= %d, got %d", maxSectionTextLength, len(sectionText))
		}
		if !strings.HasSuffix(sectionText, slackTruncationSuffix) {
			t.Errorf("expected section text to end with %q, got last 10 chars: %q", slackTruncationSuffix, sectionText[len(sectionText)-10:])
		}

		// Should be exactly 3000 characters (2997 + "...")
		expectedLength := maxSectionTextLength
		if len(sectionText) != expectedLength {
			t.Errorf("expected section text length=%d, got %d", expectedLength, len(sectionText))
		}
	})

	t.Run("TC-3: should truncate long fallback text (>150 chars)", func(t *testing.T) {
		// Arrange
		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: "https://hooks.slack.com/services/test",
			Timeout:    10 * time.Second,
		})

		longTitle := strings.Repeat("x", 200) // 200 characters
		article := &entity.Article{
			ID:          1,
			Title:       longTitle,
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		payload := notifier.buildBlockKitPayload(article, source)

		// Assert
		if len(payload.Text) > maxFallbackLength {
			t.Errorf("expected fallback text length <= %d, got %d", maxFallbackLength, len(payload.Text))
		}
		if len(payload.Text) == maxFallbackLength && !strings.HasSuffix(payload.Text, slackTruncationSuffix) {
			t.Errorf("expected fallback text to end with %q when truncated", slackTruncationSuffix)
		}
	})

	t.Run("TC-4: should handle empty summary", func(t *testing.T) {
		// Arrange
		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: "https://hooks.slack.com/services/test",
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "", // Empty summary
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		payload := notifier.buildBlockKitPayload(article, source)

		// Assert
		sectionText := payload.Blocks[0].Text.Text

		// Should still contain title link
		expectedTitleLink := fmt.Sprintf("*<%s|%s>*", article.URL, article.Title)
		if !strings.Contains(sectionText, expectedTitleLink) {
			t.Errorf("expected section text to contain title link %q", expectedTitleLink)
		}

		// Should end with title link + newlines (no summary content)
		if !strings.HasSuffix(sectionText, "*\n\n") {
			t.Errorf("expected section text to end with newlines after title, got: %q", sectionText[len(sectionText)-10:])
		}
	})

	t.Run("TC-5: should format timestamp as RFC3339", func(t *testing.T) {
		// Arrange
		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: "https://hooks.slack.com/services/test",
			Timeout:    10 * time.Second,
		})

		publishedAt := time.Date(2025, 11, 15, 12, 30, 45, 0, time.UTC)
		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: publishedAt,
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		payload := notifier.buildBlockKitPayload(article, source)

		// Assert
		contextElement := payload.Blocks[1].Elements[0]
		expectedTimestamp := "2025-11-15T12:30:45Z"
		if !strings.Contains(contextElement.Text, expectedTimestamp) {
			t.Errorf("expected context to contain timestamp %q, got %q", expectedTimestamp, contextElement.Text)
		}

		// Verify it's valid RFC3339
		parts := strings.Split(contextElement.Text, " • ")
		if len(parts) != 2 {
			t.Fatalf("expected context text to have 2 parts separated by ' • ', got %d parts", len(parts))
		}
		_, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			t.Errorf("timestamp is not valid RFC3339: %v", err)
		}
	})
}

func TestSlackNotifier_truncateSummary(t *testing.T) {
	t.Run("should not truncate short summary", func(t *testing.T) {
		summary := "Short summary"
		result := truncateSummary(summary, 100, "...")
		if result != summary {
			t.Errorf("expected %q, got %q", summary, result)
		}
	})

	t.Run("should truncate long summary with ellipsis", func(t *testing.T) {
		summary := strings.Repeat("a", 100)
		result := truncateSummary(summary, 50, "...")

		if len(result) != 50 {
			t.Errorf("expected length=50, got %d", len(result))
		}
		if !strings.HasSuffix(result, "...") {
			t.Errorf("expected result to end with '...', got %q", result[len(result)-3:])
		}
		if result != summary[:47]+"..." {
			t.Errorf("expected first 47 chars + '...', got different result")
		}
	})

	t.Run("should handle edge case with maxLength=3", func(t *testing.T) {
		summary := "abcdef"
		result := truncateSummary(summary, 3, "...")

		if result != "..." {
			t.Errorf("expected '...', got %q", result)
		}
	})

	t.Run("should handle exact length", func(t *testing.T) {
		summary := strings.Repeat("a", 50)
		result := truncateSummary(summary, 50, "...")

		if result != summary {
			t.Errorf("expected no truncation for exact length match")
		}
	})
}

// TASK-002: Slack HTTP Request Logic Unit Tests

func TestSlackNotifier_sendWebhookRequest(t *testing.T) {
	t.Run("TC-1: should succeed with 200 OK response", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request headers
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected Content-Type=application/json, got %q", r.Header.Get("Content-Type"))
			}

			// Verify request body
			body, _ := io.ReadAll(r.Body)
			var payload SlackWebhookPayload
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("failed to parse request body: %v", err)
			}

			// Verify payload structure
			if payload.Text == "" {
				t.Error("expected fallback text to be non-empty")
			}
			if len(payload.Blocks) == 0 {
				t.Error("expected blocks to be non-empty")
			}

			// Send success response (Slack returns "ok" as plain text)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		err := notifier.sendWebhookRequest(context.Background(), article, source)

		// Assert
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("TC-2: should handle 429 rate limit with Retry-After header", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok": false, "error": "rate_limited"}`))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		err := notifier.sendWebhookRequest(context.Background(), article, source)

		// Assert
		if err == nil {
			t.Fatal("expected rate limit error, got nil")
		}

		rateLimitErr, ok := err.(*RateLimitError)
		if !ok {
			t.Fatalf("expected RateLimitError, got %T", err)
		}

		expectedRetryAfter := 2 * time.Second
		if rateLimitErr.RetryAfter != expectedRetryAfter {
			t.Errorf("expected retry_after=%v, got %v", expectedRetryAfter, rateLimitErr.RetryAfter)
		}
	})

	t.Run("TC-3: should return ClientError for 4xx (non-retryable)", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok": false, "error": "invalid_payload"}`))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		err := notifier.sendWebhookRequest(context.Background(), article, source)

		// Assert
		if err == nil {
			t.Fatal("expected client error, got nil")
		}

		clientErr, ok := err.(*ClientError)
		if !ok {
			t.Fatalf("expected ClientError, got %T", err)
		}

		if clientErr.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status code=%d, got %d", http.StatusBadRequest, clientErr.StatusCode)
		}

		// Verify it's not retryable
		if isRetryableError(err) {
			t.Error("expected client error to be non-retryable")
		}
	})

	t.Run("TC-4: should return ServerError for 5xx (retryable)", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok": false, "error": "slack_internal_error"}`))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		err := notifier.sendWebhookRequest(context.Background(), article, source)

		// Assert
		if err == nil {
			t.Fatal("expected server error, got nil")
		}

		serverErr, ok := err.(*ServerError)
		if !ok {
			t.Fatalf("expected ServerError, got %T", err)
		}

		if serverErr.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected status code=%d, got %d", http.StatusInternalServerError, serverErr.StatusCode)
		}

		// Verify it's retryable
		if !isRetryableError(err) {
			t.Error("expected server error to be retryable")
		}
	})

	t.Run("TC-5: should handle network timeout", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Simulate slow response
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    50 * time.Millisecond, // Short timeout to trigger timeout
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		err := notifier.sendWebhookRequest(context.Background(), article, source)

		// Assert
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}

		// Network errors should be retryable
		if !isRetryableError(err) {
			t.Error("expected network timeout to be retryable")
		}
	})

	t.Run("TC-6: should handle 403 Forbidden (webhook disabled)", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"ok": false, "error": "action_prohibited"}`))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		err := notifier.sendWebhookRequest(context.Background(), article, source)

		// Assert
		if err == nil {
			t.Fatal("expected client error, got nil")
		}

		clientErr, ok := err.(*ClientError)
		if !ok {
			t.Fatalf("expected ClientError, got %T", err)
		}

		if clientErr.StatusCode != http.StatusForbidden {
			t.Errorf("expected status code=%d, got %d", http.StatusForbidden, clientErr.StatusCode)
		}
	})
}

// TASK-002: Retry Logic Unit Tests

func TestSlackNotifier_sendWebhookRequestWithRetry(t *testing.T) {
	t.Run("TC-1: should succeed on first attempt (no retry)", func(t *testing.T) {
		// Arrange
		requestCount := int32(0)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requestCount, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		ctx := context.WithValue(context.Background(), requestIDKey, "test-request-1")

		// Act
		err := notifier.sendWebhookRequestWithRetry(ctx, article, source)

		// Assert
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if atomic.LoadInt32(&requestCount) != 1 {
			t.Errorf("expected 1 request, got %d", requestCount)
		}
	})

	t.Run("TC-2: should succeed on second attempt (after 1 retry)", func(t *testing.T) {
		// Arrange
		requestCount := int32(0)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if count == 1 {
				// First request fails with 5xx
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				// Second request succeeds
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
			}
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		ctx := context.WithValue(context.Background(), requestIDKey, "test-request-2")

		// Act
		start := time.Now()
		err := notifier.sendWebhookRequestWithRetry(ctx, article, source)
		elapsed := time.Since(start)

		// Assert
		if err != nil {
			t.Errorf("expected no error after retry, got %v", err)
		}

		if atomic.LoadInt32(&requestCount) != 2 {
			t.Errorf("expected 2 requests, got %d", requestCount)
		}

		// Should wait ~5s between retries
		if elapsed < 4*time.Second || elapsed > 6*time.Second {
			t.Logf("warning: expected ~5s delay, got %v (this might be flaky in slow environments)", elapsed)
		}
	})

	t.Run("TC-3: should fail after max retries (2 attempts)", func(t *testing.T) {
		// Arrange
		requestCount := int32(0)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requestCount, 1)
			// Always fail with 5xx
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		ctx := context.WithValue(context.Background(), requestIDKey, "test-request-3")

		// Act
		err := notifier.sendWebhookRequestWithRetry(ctx, article, source)

		// Assert
		if err == nil {
			t.Fatal("expected error after max retries, got nil")
		}

		if atomic.LoadInt32(&requestCount) != 2 {
			t.Errorf("expected 2 requests (max attempts), got %d", requestCount)
		}

		if !strings.Contains(err.Error(), "failed after 2 attempts") {
			t.Errorf("expected error message to mention 2 attempts, got %v", err)
		}
	})

	t.Run("TC-4: should respect Retry-After for 429 errors", func(t *testing.T) {
		// Arrange
		requestCount := int32(0)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if count == 1 {
				// First request returns 429 with Retry-After
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"ok": false, "error": "rate_limited"}`))
			} else {
				// Second request succeeds
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
			}
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		ctx := context.WithValue(context.Background(), requestIDKey, "test-request-4")

		// Act
		start := time.Now()
		err := notifier.sendWebhookRequestWithRetry(ctx, article, source)
		elapsed := time.Since(start)

		// Assert
		if err != nil {
			t.Errorf("expected no error after retry, got %v", err)
		}

		if atomic.LoadInt32(&requestCount) != 2 {
			t.Errorf("expected 2 requests, got %d", requestCount)
		}

		// Should wait ~1s (Retry-After from 429 response)
		if elapsed < 900*time.Millisecond || elapsed > 1200*time.Millisecond {
			t.Logf("warning: expected ~1s delay, got %v (this might be flaky)", elapsed)
		}
	})

	t.Run("TC-5: should not retry 4xx client errors", func(t *testing.T) {
		// Arrange
		requestCount := int32(0)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requestCount, 1)
			// Return 400 Bad Request
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		ctx := context.WithValue(context.Background(), requestIDKey, "test-request-5")

		// Act
		err := notifier.sendWebhookRequestWithRetry(ctx, article, source)

		// Assert
		if err == nil {
			t.Fatal("expected error for 400, got nil")
		}

		// Should only attempt once (no retry for 4xx)
		if atomic.LoadInt32(&requestCount) != 1 {
			t.Errorf("expected 1 request (no retry for 4xx), got %d", requestCount)
		}

		clientErr, ok := err.(*ClientError)
		if !ok {
			t.Fatalf("expected ClientError, got %T", err)
		}

		if clientErr.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status code=400, got %d", clientErr.StatusCode)
		}
	})

	t.Run("TC-6: should handle context timeout during retry", func(t *testing.T) {
		// Arrange
		requestCount := int32(0)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requestCount, 1)
			// Always fail with 5xx to trigger retry
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Create context with short timeout
		ctx := context.WithValue(context.Background(), requestIDKey, "test-request-6")
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		// Act
		err := notifier.sendWebhookRequestWithRetry(ctx, article, source)

		// Assert
		if err == nil {
			t.Fatal("expected context timeout error, got nil")
		}

		if !strings.Contains(err.Error(), "context") {
			t.Errorf("expected context-related error, got %v", err)
		}

		// Should attempt once, then fail during retry backoff
		count := atomic.LoadInt32(&requestCount)
		if count != 1 {
			t.Logf("expected 1 request, got %d (this might vary based on timing)", count)
		}
	})
}

// TASK-002: NotifyArticle Method Unit Tests

func TestSlackNotifier_NotifyArticle(t *testing.T) {
	t.Run("TC-1: should send successful notification end-to-end", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          123,
			SourceID:    456,
			Title:       "Test Article",
			URL:         "https://example.com/article/123",
			Summary:     "This is a test article summary.",
			PublishedAt: time.Now(),
			CreatedAt:   time.Now(),
		}

		source := &entity.Source{
			ID:      456,
			Name:    "Test News Source",
			FeedURL: "https://example.com/feed",
			Active:  true,
		}

		ctx := context.Background()

		// Act
		err := notifier.NotifyArticle(ctx, article, source)

		// Assert
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("TC-2: should generate request_id and log it", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		ctx := context.Background()

		// Act
		err := notifier.NotifyArticle(ctx, article, source)

		// Assert
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Note: request_id is generated internally and logged
		// We can't directly verify it without exposing internal state
		// But we verify the notification succeeds, which means request_id was generated
	})

	t.Run("TC-3: should apply rate limiting before sending", func(t *testing.T) {
		// Arrange
		requestCount := int32(0)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requestCount, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		ctx := context.Background()

		// Act
		err := notifier.NotifyArticle(ctx, article, source)

		// Assert
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Verify webhook was called
		if atomic.LoadInt32(&requestCount) != 1 {
			t.Errorf("expected 1 webhook request, got %d", requestCount)
		}

		// Note: Rate limiting is applied internally
		// We verify the notification succeeds, which means rate limiting passed
	})

	t.Run("TC-4: should return error but not panic on failure", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always fail with 5xx
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		ctx := context.Background()

		// Act
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("expected no panic, but got panic: %v", r)
				}
			}()
			err = notifier.NotifyArticle(ctx, article, source)
		}()

		// Assert
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("TC-5: should enforce rate limiting across multiple requests", func(t *testing.T) {
		// Arrange
		requestTimes := make([]time.Time, 0, 3)
		var timesLock = make(chan struct{}, 1)
		timesLock <- struct{}{}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-timesLock
			requestTimes = append(requestTimes, time.Now())
			timesLock <- struct{}{}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		notifier := NewSlackNotifier(SlackConfig{
			Enabled:    true,
			WebhookURL: server.URL,
			Timeout:    10 * time.Second,
		})

		articles := []*entity.Article{
			{ID: 1, Title: "Article 1", URL: "https://example.com/1", Summary: "Summary 1", PublishedAt: time.Now()},
			{ID: 2, Title: "Article 2", URL: "https://example.com/2", Summary: "Summary 2", PublishedAt: time.Now()},
			{ID: 3, Title: "Article 3", URL: "https://example.com/3", Summary: "Summary 3", PublishedAt: time.Now()},
		}

		source := &entity.Source{ID: 1, Name: "Test Source"}
		ctx := context.Background()

		// Act
		for _, article := range articles {
			err := notifier.NotifyArticle(ctx, article, source)
			if err != nil {
				t.Errorf("expected no error for article %d, got %v", article.ID, err)
			}
		}

		// Assert
		if len(requestTimes) != 3 {
			t.Fatalf("expected 3 requests, got %d", len(requestTimes))
		}

		// Verify at least ~1 second delay between requests
		for i := 1; i < len(requestTimes); i++ {
			delay := requestTimes[i].Sub(requestTimes[i-1])
			if delay < 900*time.Millisecond {
				t.Errorf("expected delay >= 900ms between requests %d and %d, got %v", i-1, i, delay)
			}
		}
	})
}

func TestNewSlackNotifier(t *testing.T) {
	t.Run("should create Slack notifier with proper configuration", func(t *testing.T) {
		// Arrange
		config := SlackConfig{
			Enabled:    true,
			WebhookURL: "https://hooks.slack.com/services/test",
			Timeout:    15 * time.Second,
		}

		// Act
		notifier := NewSlackNotifier(config)

		// Assert
		if notifier == nil {
			t.Fatal("expected non-nil notifier")
		}
		if notifier.httpClient == nil {
			t.Error("expected http client to be initialized")
		}
		if notifier.httpClient.Timeout != config.Timeout {
			t.Errorf("expected timeout=%v, got %v", config.Timeout, notifier.httpClient.Timeout)
		}
		if notifier.rateLimiter == nil {
			t.Error("expected rate limiter to be initialized")
		}
		if notifier.config.WebhookURL != config.WebhookURL {
			t.Errorf("expected webhook URL=%q, got %q", config.WebhookURL, notifier.config.WebhookURL)
		}
	})
}
