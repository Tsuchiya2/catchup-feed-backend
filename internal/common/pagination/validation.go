package pagination

import "fmt"

// Validate validates pagination parameters against the configuration.
// Returns an error if:
//   - page is less than 1
//   - limit is less than 1 or greater than config.MaxLimit
func (p Params) Validate(config Config) error {
	if p.Page < 1 {
		return fmt.Errorf("page must be a positive integer")
	}
	if p.Limit < 1 || p.Limit > config.MaxLimit {
		return fmt.Errorf("limit must be between 1 and %d", config.MaxLimit)
	}
	return nil
}

// WithDefaults applies default values from config to Params.
// This is useful for ensuring params have valid values.
//
// Rules:
//   - If page <= 0, set to config.DefaultPage
//   - If limit <= 0, set to config.DefaultLimit
//   - If limit > config.MaxLimit, cap to config.MaxLimit
func (p Params) WithDefaults(config Config) Params {
	if p.Page <= 0 {
		p.Page = config.DefaultPage
	}
	if p.Limit <= 0 {
		p.Limit = config.DefaultLimit
	}
	if p.Limit > config.MaxLimit {
		p.Limit = config.MaxLimit
	}
	return p
}
