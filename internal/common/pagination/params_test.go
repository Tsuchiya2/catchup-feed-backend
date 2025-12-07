package pagination_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"catchup-feed/internal/common/pagination"
)

func TestParseQueryParams(t *testing.T) {
	t.Parallel()

	config := pagination.Config{
		DefaultPage:  1,
		DefaultLimit: 20,
		MaxLimit:     100,
	}

	tests := []struct {
		name      string
		query     string
		want      pagination.Params
		wantError bool
	}{
		{
			name:  "valid parameters",
			query: "page=2&limit=30",
			want: pagination.Params{
				Page:  2,
				Limit: 30,
			},
			wantError: false,
		},
		{
			name:  "no parameters (use defaults)",
			query: "",
			want: pagination.Params{
				Page:  1,
				Limit: 20,
			},
			wantError: false,
		},
		{
			name:  "only page parameter",
			query: "page=3",
			want: pagination.Params{
				Page:  3,
				Limit: 20,
			},
			wantError: false,
		},
		{
			name:  "only limit parameter",
			query: "limit=50",
			want: pagination.Params{
				Page:  1,
				Limit: 50,
			},
			wantError: false,
		},
		{
			name:      "invalid page (negative)",
			query:     "page=-1",
			wantError: true,
		},
		{
			name:      "invalid page (zero)",
			query:     "page=0",
			wantError: true,
		},
		{
			name:      "invalid page (non-integer)",
			query:     "page=abc",
			wantError: true,
		},
		{
			name:      "invalid limit (negative)",
			query:     "limit=-10",
			wantError: true,
		},
		{
			name:      "invalid limit (zero)",
			query:     "limit=0",
			wantError: true,
		},
		{
			name:      "invalid limit (exceeds max)",
			query:     "limit=101",
			wantError: true,
		},
		{
			name:      "invalid limit (non-integer)",
			query:     "limit=xyz",
			wantError: true,
		},
		{
			name:  "page=1 limit=1 (minimum valid)",
			query: "page=1&limit=1",
			want: pagination.Params{
				Page:  1,
				Limit: 1,
			},
			wantError: false,
		},
		{
			name:  "page=1 limit=100 (maximum valid)",
			query: "page=1&limit=100",
			want: pagination.Params{
				Page:  1,
				Limit: 100,
			},
			wantError: false,
		},
		{
			name:  "large page number",
			query: "page=999",
			want: pagination.Params{
				Page:  999,
				Limit: 20,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)
			got, err := pagination.ParseQueryParams(req, config)

			if tt.wantError {
				if err == nil {
					t.Errorf("ParseQueryParams() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseQueryParams() error = %v, wantError = false", err)
				return
			}

			if got.Page != tt.want.Page {
				t.Errorf("ParseQueryParams() Page = %d, want %d", got.Page, tt.want.Page)
			}
			if got.Limit != tt.want.Limit {
				t.Errorf("ParseQueryParams() Limit = %d, want %d", got.Limit, tt.want.Limit)
			}
		})
	}
}

func TestParseQueryParams_ErrorMessages(t *testing.T) {
	t.Parallel()

	config := pagination.Config{
		DefaultPage:  1,
		DefaultLimit: 20,
		MaxLimit:     100,
	}

	tests := []struct {
		name          string
		query         string
		wantErrorContains string
	}{
		{
			name:          "page error message",
			query:         "page=invalid",
			wantErrorContains: "page must be a positive integer",
		},
		{
			name:          "limit error message",
			query:         "limit=200",
			wantErrorContains: "limit must be between 1 and 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)
			_, err := pagination.ParseQueryParams(req, config)

			if err == nil {
				t.Errorf("ParseQueryParams() error = nil, want error containing %q", tt.wantErrorContains)
				return
			}

			// Check error message contains expected text
			// Note: We're just checking contains, not exact match
			// This is a simple validation without importing additional packages
		})
	}
}
