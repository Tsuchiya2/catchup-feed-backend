package source

import "time"

// DTO mirrors the §4 sources schema (+ Phase 2 kind). Category drives the
// radio script corner assignment; Lang defaults to 'en'; Kind is the
// content pipeline selector (rss | youtube | podcast).
type DTO struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	FeedURL   string    `json:"feed_url"`
	URL       string    `json:"url"` // Mapped from FeedURL for frontend compatibility
	Category  string    `json:"category"`
	Lang      string    `json:"lang"`
	Kind      string    `json:"kind" example:"rss" enums:"rss,youtube,podcast"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateRequest is the POST /sources body. name / feedURL / category are
// required; lang defaults to 'en' when empty.
type CreateRequest struct {
	Name     string `json:"name" example:"Go Blog"`
	FeedURL  string `json:"feedURL" example:"https://go.dev/blog/feed.atom"`
	Category string `json:"category" example:"go"`
	Lang     string `json:"lang,omitempty" example:"en"`
	Kind     string `json:"kind,omitempty" example:"rss" enums:"rss,youtube,podcast"`
}

// UpdateRequest is the PUT /sources/{id} body. Empty strings keep the
// current value; active is optional (null = unchanged).
type UpdateRequest struct {
	Name     string `json:"name,omitempty" example:"Go Blog"`
	FeedURL  string `json:"feedURL,omitempty" example:"https://go.dev/blog/feed.atom"`
	Category string `json:"category,omitempty" example:"go"`
	Lang     string `json:"lang,omitempty" example:"en"`
	Kind     string `json:"kind,omitempty" example:"rss" enums:"rss,youtube,podcast"`
	Active   *bool  `json:"active,omitempty" example:"true"`
}

// fromEntityFields builds a DTO from the source entity fields shared by
// list and search responses.
func toDTO(id int64, name, feedURL, category, lang, kind string, active bool, createdAt time.Time) DTO {
	return DTO{
		ID:        id,
		Name:      name,
		FeedURL:   feedURL,
		URL:       feedURL, // Map FeedURL to URL for frontend compatibility
		Category:  category,
		Lang:      lang,
		Kind:      kind,
		Active:    active,
		CreatedAt: createdAt,
	}
}
