package metrics

import (
	"fmt"
	"time"
)

// RecordArticlesFetched records the number of articles fetched from a source.
// This metric helps track feed crawling performance and source activity.
func RecordArticlesFetched(sourceName string, sourceID int64, count int) {
	ArticlesFetchedTotal.WithLabelValues(
		sourceName,
		fmt.Sprintf("%d", sourceID),
	).Add(float64(count))
}

// RecordArticleSummarized records the result of an article summarization operation.
// Status should be either "success" or "failure".
func RecordArticleSummarized(success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	ArticlesSummarizedTotal.WithLabelValues(status).Inc()
}

// RecordSummarizationDuration records the time taken to summarize an article.
// This helps identify performance issues with the AI summarization service.
func RecordSummarizationDuration(duration time.Duration) {
	SummarizationDuration.Observe(duration.Seconds())
}

// RecordFeedCrawl records metrics for a feed crawl operation.
func RecordFeedCrawl(sourceID int64, duration time.Duration, itemsFound, itemsInserted, itemsDuplicated int64) {
	FeedCrawlDuration.WithLabelValues(
		fmt.Sprintf("%d", sourceID),
	).Observe(duration.Seconds())

	// Record the breakdown of items processed
	if itemsFound > 0 {
		RecordArticlesFetched("", sourceID, int(itemsFound))
	}
}

// RecordFeedCrawlError records an error during feed crawling.
func RecordFeedCrawlError(sourceID int64, errorType string) {
	FeedCrawlErrors.WithLabelValues(
		fmt.Sprintf("%d", sourceID),
		errorType,
	).Inc()
}

// UpdateArticlesTotal updates the total count of articles in the database.
// This gauge should be updated periodically to reflect the current state.
func UpdateArticlesTotal(count int) {
	ArticlesTotal.Set(float64(count))
}

// UpdateSourcesTotal updates the total count of sources in the database.
// This gauge should be updated periodically to reflect the current state.
func UpdateSourcesTotal(count int) {
	SourcesTotal.Set(float64(count))
}

// RecordContentFetchSuccess records a successful content fetch operation.
// This tracks both the duration and size of fetched content.
//
// Parameters:
//   - duration: Time taken to fetch the content
//   - size: Size of fetched content in characters
//
// Example:
//
//	start := time.Now()
//	content, err := fetcher.FetchContent(ctx, url)
//	if err == nil {
//	    RecordContentFetchSuccess(time.Since(start), len(content))
//	}
func RecordContentFetchSuccess(duration time.Duration, size int) {
	ContentFetchAttemptsTotal.WithLabelValues("success").Inc()
	ContentFetchDuration.Observe(duration.Seconds())
	ContentFetchSize.Observe(float64(size))
}

// RecordContentFetchFailed records a failed content fetch operation.
//
// Parameters:
//   - duration: Time taken before the fetch failed
//
// Example:
//
//	start := time.Now()
//	_, err := fetcher.FetchContent(ctx, url)
//	if err != nil {
//	    RecordContentFetchFailed(time.Since(start))
//	}
func RecordContentFetchFailed(duration time.Duration) {
	ContentFetchAttemptsTotal.WithLabelValues("failure").Inc()
	ContentFetchDuration.Observe(duration.Seconds())
}

// RecordContentFetchSkipped records a skipped content fetch operation.
// This occurs when RSS content is sufficient (>= threshold) and fetching is unnecessary.
//
// Example:
//
//	if len(rssContent) >= threshold {
//	    RecordContentFetchSkipped()
//	    return rssContent
//	}
func RecordContentFetchSkipped() {
	ContentFetchAttemptsTotal.WithLabelValues("skipped").Inc()
}

// RecordDBQuery records the duration of a database query operation.
// Operation should describe the query type (e.g., "select_articles", "insert_article").
func RecordDBQuery(operation string, duration time.Duration) {
	DBQueryDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// UpdateDBConnectionStats updates database connection pool statistics.
func UpdateDBConnectionStats(active, idle int) {
	DBConnectionsActive.Set(float64(active))
	DBConnectionsIdle.Set(float64(idle))
}
