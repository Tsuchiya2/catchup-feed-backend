package source

import "time"

// DTO mirrors the §4 sources schema. Category drives the radio script
// corner assignment; Lang defaults to 'en'.
type DTO struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	FeedURL   string    `json:"feed_url"`
	URL       string    `json:"url"` // Mapped from FeedURL for frontend compatibility
	Category  string    `json:"category"`
	Lang      string    `json:"lang"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// fromEntityFields builds a DTO from the source entity fields shared by
// list and search responses.
func toDTO(id int64, name, feedURL, category, lang string, active bool, createdAt time.Time) DTO {
	return DTO{
		ID:        id,
		Name:      name,
		FeedURL:   feedURL,
		URL:       feedURL, // Map FeedURL to URL for frontend compatibility
		Category:  category,
		Lang:      lang,
		Active:    active,
		CreatedAt: createdAt,
	}
}
