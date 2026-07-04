package entity

import "time"

// Summary provider names persisted in summaries.provider (§4). They mirror
// the summarizer fallback chain (Gemini -> Groq -> Ollama) and exist so that
// fallback occurrences are observable after the fact (§8).
const (
	// SummaryProviderUnknown is stored when the summarizer implementation
	// cannot report a provider name (e.g. a plain Summarizer stub).
	SummaryProviderUnknown = "unknown"
)

// Summary represents the Japanese summary of an article (summaries table,
// §4). article_id is the primary key: one summary per article, upserted.
type Summary struct {
	ArticleID int64
	Body      string
	Provider  string // gemini / groq / ollama (フォールバック観測用)
	CreatedAt time.Time
}
