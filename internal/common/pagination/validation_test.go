package pagination_test

import (
	"testing"

	"catchup-feed/internal/common/pagination"
)

func TestParams_Validate(t *testing.T) {
	t.Parallel()

	config := pagination.Config{
		DefaultPage:  1,
		DefaultLimit: 20,
		MaxLimit:     100,
	}

	tests := []struct {
		name      string
		params    pagination.Params
		wantError bool
	}{
		{
			name: "valid params",
			params: pagination.Params{
				Page:  1,
				Limit: 20,
			},
			wantError: false,
		},
		{
			name: "valid params with limit at max",
			params: pagination.Params{
				Page:  1,
				Limit: 100,
			},
			wantError: false,
		},
		{
			name: "valid params with limit at min",
			params: pagination.Params{
				Page:  1,
				Limit: 1,
			},
			wantError: false,
		},
		{
			name: "invalid page (zero)",
			params: pagination.Params{
				Page:  0,
				Limit: 20,
			},
			wantError: true,
		},
		{
			name: "invalid page (negative)",
			params: pagination.Params{
				Page:  -1,
				Limit: 20,
			},
			wantError: true,
		},
		{
			name: "invalid limit (zero)",
			params: pagination.Params{
				Page:  1,
				Limit: 0,
			},
			wantError: true,
		},
		{
			name: "invalid limit (negative)",
			params: pagination.Params{
				Page:  1,
				Limit: -10,
			},
			wantError: true,
		},
		{
			name: "invalid limit (exceeds max)",
			params: pagination.Params{
				Page:  1,
				Limit: 101,
			},
			wantError: true,
		},
		{
			name: "both page and limit invalid",
			params: pagination.Params{
				Page:  0,
				Limit: 0,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate(config)

			if tt.wantError && err == nil {
				t.Errorf("Validate() error = nil, wantError = true")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Validate() error = %v, wantError = false", err)
			}
		})
	}
}

func TestParams_WithDefaults(t *testing.T) {
	t.Parallel()

	config := pagination.Config{
		DefaultPage:  1,
		DefaultLimit: 20,
		MaxLimit:     100,
	}

	tests := []struct {
		name   string
		params pagination.Params
		want   pagination.Params
	}{
		{
			name: "valid params unchanged",
			params: pagination.Params{
				Page:  2,
				Limit: 30,
			},
			want: pagination.Params{
				Page:  2,
				Limit: 30,
			},
		},
		{
			name: "zero page gets default",
			params: pagination.Params{
				Page:  0,
				Limit: 30,
			},
			want: pagination.Params{
				Page:  1,
				Limit: 30,
			},
		},
		{
			name: "negative page gets default",
			params: pagination.Params{
				Page:  -5,
				Limit: 30,
			},
			want: pagination.Params{
				Page:  1,
				Limit: 30,
			},
		},
		{
			name: "zero limit gets default",
			params: pagination.Params{
				Page:  2,
				Limit: 0,
			},
			want: pagination.Params{
				Page:  2,
				Limit: 20,
			},
		},
		{
			name: "negative limit gets default",
			params: pagination.Params{
				Page:  2,
				Limit: -10,
			},
			want: pagination.Params{
				Page:  2,
				Limit: 20,
			},
		},
		{
			name: "limit exceeds max gets capped",
			params: pagination.Params{
				Page:  2,
				Limit: 200,
			},
			want: pagination.Params{
				Page:  2,
				Limit: 100,
			},
		},
		{
			name: "both page and limit invalid get defaults",
			params: pagination.Params{
				Page:  0,
				Limit: 0,
			},
			want: pagination.Params{
				Page:  1,
				Limit: 20,
			},
		},
		{
			name: "limit at max stays unchanged",
			params: pagination.Params{
				Page:  2,
				Limit: 100,
			},
			want: pagination.Params{
				Page:  2,
				Limit: 100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.params.WithDefaults(config)

			if got.Page != tt.want.Page {
				t.Errorf("WithDefaults() Page = %d, want %d", got.Page, tt.want.Page)
			}
			if got.Limit != tt.want.Limit {
				t.Errorf("WithDefaults() Limit = %d, want %d", got.Limit, tt.want.Limit)
			}
		})
	}
}
