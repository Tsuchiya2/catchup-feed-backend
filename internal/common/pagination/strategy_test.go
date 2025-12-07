package pagination_test

import (
	"testing"

	"catchup-feed/internal/common/pagination"
)

func TestOffsetStrategy_CalculateQuery(t *testing.T) {
	t.Parallel()

	strategy := pagination.OffsetStrategy{}

	tests := []struct {
		name   string
		params pagination.Params
		want   pagination.QueryParams
	}{
		{
			name: "first page",
			params: pagination.Params{
				Page:  1,
				Limit: 20,
			},
			want: pagination.QueryParams{
				Offset: 0,
				Limit:  20,
				Cursor: nil,
				After:  nil,
			},
		},
		{
			name: "second page",
			params: pagination.Params{
				Page:  2,
				Limit: 20,
			},
			want: pagination.QueryParams{
				Offset: 20,
				Limit:  20,
				Cursor: nil,
				After:  nil,
			},
		},
		{
			name: "page 5 with limit 50",
			params: pagination.Params{
				Page:  5,
				Limit: 50,
			},
			want: pagination.QueryParams{
				Offset: 200,
				Limit:  50,
				Cursor: nil,
				After:  nil,
			},
		},
		{
			name: "large page number",
			params: pagination.Params{
				Page:  100,
				Limit: 10,
			},
			want: pagination.QueryParams{
				Offset: 990,
				Limit:  10,
				Cursor: nil,
				After:  nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strategy.CalculateQuery(tt.params)

			if got.Offset != tt.want.Offset {
				t.Errorf("CalculateQuery() Offset = %d, want %d", got.Offset, tt.want.Offset)
			}
			if got.Limit != tt.want.Limit {
				t.Errorf("CalculateQuery() Limit = %d, want %d", got.Limit, tt.want.Limit)
			}
			if got.Cursor != nil {
				t.Errorf("CalculateQuery() Cursor = %v, want nil", got.Cursor)
			}
			if got.After != nil {
				t.Errorf("CalculateQuery() After = %v, want nil", got.After)
			}
		})
	}
}

func BenchmarkOffsetStrategy_CalculateQuery(b *testing.B) {
	strategy := pagination.OffsetStrategy{}
	params := pagination.Params{
		Page:  10,
		Limit: 20,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		strategy.CalculateQuery(params)
	}
}
