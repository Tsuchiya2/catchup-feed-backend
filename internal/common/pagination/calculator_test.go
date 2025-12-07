package pagination_test

import (
	"testing"

	"catchup-feed/internal/common/pagination"
)

func TestCalculateOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		page  int
		limit int
		want  int
	}{
		{
			name:  "first page",
			page:  1,
			limit: 20,
			want:  0,
		},
		{
			name:  "second page",
			page:  2,
			limit: 20,
			want:  20,
		},
		{
			name:  "third page",
			page:  3,
			limit: 20,
			want:  40,
		},
		{
			name:  "page 10 with limit 50",
			page:  10,
			limit: 50,
			want:  450,
		},
		{
			name:  "page 1 with limit 1",
			page:  1,
			limit: 1,
			want:  0,
		},
		{
			name:  "page 100 with limit 10",
			page:  100,
			limit: 10,
			want:  990,
		},
		{
			name:  "large page number",
			page:  1000,
			limit: 20,
			want:  19980,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pagination.CalculateOffset(tt.page, tt.limit)
			if got != tt.want {
				t.Errorf("CalculateOffset(%d, %d) = %d, want %d", tt.page, tt.limit, got, tt.want)
			}
		})
	}
}

func TestCalculateTotalPages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		total int64
		limit int
		want  int
	}{
		{
			name:  "zero total",
			total: 0,
			limit: 20,
			want:  1,
		},
		{
			name:  "total less than limit",
			total: 10,
			limit: 20,
			want:  1,
		},
		{
			name:  "total equals limit",
			total: 20,
			limit: 20,
			want:  1,
		},
		{
			name:  "total one more than limit",
			total: 21,
			limit: 20,
			want:  2,
		},
		{
			name:  "total exactly 2 pages",
			total: 40,
			limit: 20,
			want:  2,
		},
		{
			name:  "total 2 pages plus 1",
			total: 41,
			limit: 20,
			want:  3,
		},
		{
			name:  "total 150 with limit 20",
			total: 150,
			limit: 20,
			want:  8,
		},
		{
			name:  "total 151 with limit 20",
			total: 151,
			limit: 20,
			want:  8,
		},
		{
			name:  "total 159 with limit 20",
			total: 159,
			limit: 20,
			want:  8,
		},
		{
			name:  "total 160 with limit 20",
			total: 160,
			limit: 20,
			want:  8,
		},
		{
			name:  "total 161 with limit 20",
			total: 161,
			limit: 20,
			want:  9,
		},
		{
			name:  "large total",
			total: 10000,
			limit: 100,
			want:  100,
		},
		{
			name:  "large total with small limit",
			total: 9999,
			limit: 10,
			want:  1000,
		},
		{
			name:  "limit 1",
			total: 5,
			limit: 1,
			want:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pagination.CalculateTotalPages(tt.total, tt.limit)
			if got != tt.want {
				t.Errorf("CalculateTotalPages(%d, %d) = %d, want %d", tt.total, tt.limit, got, tt.want)
			}
		})
	}
}

// Benchmark tests
func BenchmarkCalculateOffset(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pagination.CalculateOffset(100, 20)
	}
}

func BenchmarkCalculateTotalPages(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pagination.CalculateTotalPages(10000, 20)
	}
}
