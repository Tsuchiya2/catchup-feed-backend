package sqlite_test

import (
	"testing"
	"time"

	"catchup-feed/internal/infra/adapter/persistence/sqlite"
	"catchup-feed/internal/repository"
)

/* ──────────────────────────── QueryBuilder Tests ──────────────────────────── */

func TestQueryBuilder_BuildWhereClause_KeywordOnly(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	// Test single keyword
	clause, args := qb.BuildWhereClause([]string{"golang"}, repository.ArticleSearchFilters{})

	expectedClause := "WHERE (title LIKE ? OR summary LIKE ?)"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	if len(args) != 2 {
		t.Fatalf("args length = %d, want 2", len(args))
	}
	if args[0] != "%golang%" || args[1] != "%golang%" {
		t.Errorf("args = %v, want [%%golang%% %%golang%%]", args)
	}
}

func TestQueryBuilder_BuildWhereClause_MultipleKeywords(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	// Test multiple keywords (AND logic)
	clause, args := qb.BuildWhereClause([]string{"golang", "testing"}, repository.ArticleSearchFilters{})

	expectedClause := "WHERE (title LIKE ? OR summary LIKE ?) AND (title LIKE ? OR summary LIKE ?)"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	if len(args) != 4 {
		t.Fatalf("args length = %d, want 4", len(args))
	}

	expectedArgs := []interface{}{"%golang%", "%golang%", "%testing%", "%testing%"}
	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Errorf("args[%d] = %v, want %v", i, arg, expectedArgs[i])
		}
	}
}

func TestQueryBuilder_BuildWhereClause_SourceIDOnly(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	sourceID := int64(123)
	filters := repository.ArticleSearchFilters{
		SourceID: &sourceID,
	}

	clause, args := qb.BuildWhereClause([]string{}, filters)

	expectedClause := "WHERE source_id = ?"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	if len(args) != 1 {
		t.Fatalf("args length = %d, want 1", len(args))
	}
	if args[0] != int64(123) {
		t.Errorf("args[0] = %v, want 123", args[0])
	}
}

func TestQueryBuilder_BuildWhereClause_DateRangeOnly(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)

	filters := repository.ArticleSearchFilters{
		From: &from,
		To:   &to,
	}

	clause, args := qb.BuildWhereClause([]string{}, filters)

	expectedClause := "WHERE published_at >= ? AND published_at <= ?"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	if len(args) != 2 {
		t.Fatalf("args length = %d, want 2", len(args))
	}
	if args[0] != from {
		t.Errorf("args[0] = %v, want %v", args[0], from)
	}
	if args[1] != to {
		t.Errorf("args[1] = %v, want %v", args[1], to)
	}
}

func TestQueryBuilder_BuildWhereClause_FromDateOnly(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	filters := repository.ArticleSearchFilters{
		From: &from,
	}

	clause, args := qb.BuildWhereClause([]string{}, filters)

	expectedClause := "WHERE published_at >= ?"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	if len(args) != 1 {
		t.Fatalf("args length = %d, want 1", len(args))
	}
}

func TestQueryBuilder_BuildWhereClause_ToDateOnly(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	to := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)

	filters := repository.ArticleSearchFilters{
		To: &to,
	}

	clause, args := qb.BuildWhereClause([]string{}, filters)

	expectedClause := "WHERE published_at <= ?"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	if len(args) != 1 {
		t.Fatalf("args length = %d, want 1", len(args))
	}
}

func TestQueryBuilder_BuildWhereClause_AllFilters(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	sourceID := int64(456)
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)

	filters := repository.ArticleSearchFilters{
		SourceID: &sourceID,
		From:     &from,
		To:       &to,
	}

	clause, args := qb.BuildWhereClause([]string{"golang", "api"}, filters)

	expectedClause := "WHERE (title LIKE ? OR summary LIKE ?) AND (title LIKE ? OR summary LIKE ?) AND source_id = ? AND published_at >= ? AND published_at <= ?"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	// 2 keywords * 2 (title + summary) + 1 source_id + 2 date range = 7 args
	if len(args) != 7 {
		t.Fatalf("args length = %d, want 7", len(args))
	}

	// Verify args order
	expectedArgs := []interface{}{
		"%golang%", "%golang%",
		"%api%", "%api%",
		int64(456),
		from,
		to,
	}

	for i, arg := range args {
		if arg != expectedArgs[i] {
			t.Errorf("args[%d] = %v, want %v", i, arg, expectedArgs[i])
		}
	}
}

func TestQueryBuilder_BuildWhereClause_EmptyFilters(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	// No keywords, no filters -> returns empty string
	clause, args := qb.BuildWhereClause([]string{}, repository.ArticleSearchFilters{})

	if clause != "" {
		t.Errorf("clause = %q, want empty string", clause)
	}

	if len(args) != 0 {
		t.Errorf("args length = %d, want 0", len(args))
	}
}

func TestQueryBuilder_BuildWhereClause_SpecialCharacters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyword string
	}{
		{"underscore", "test_keyword"},
		{"percent", "test%keyword"},
		{"single quote", "test'keyword"},
		{"double quote", "test\"keyword"},
		{"backslash", "test\\keyword"},
		{"unicode", "テスト"},
		{"emoji", "test 🔥 keyword"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			qb := sqlite.NewArticleQueryBuilder()

			clause, args := qb.BuildWhereClause([]string{tt.keyword}, repository.ArticleSearchFilters{})

			expectedClause := "WHERE (title LIKE ? OR summary LIKE ?)"
			if clause != expectedClause {
				t.Errorf("clause = %q, want %q", clause, expectedClause)
			}

			// Special characters should be wrapped in %...% but not escaped here
			// Escaping is handled by the database driver
			expectedPattern := "%" + tt.keyword + "%"
			if args[0] != expectedPattern {
				t.Errorf("args[0] = %v, want %v", args[0], expectedPattern)
			}
		})
	}
}

func TestQueryBuilder_BuildWhereClause_EmptyKeyword(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	// Empty string keyword should still generate WHERE clause
	clause, args := qb.BuildWhereClause([]string{""}, repository.ArticleSearchFilters{})

	expectedClause := "WHERE (title LIKE ? OR summary LIKE ?)"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	if len(args) != 2 {
		t.Fatalf("args length = %d, want 2", len(args))
	}

	// Empty keyword becomes "%%"
	if args[0] != "%%" || args[1] != "%%" {
		t.Errorf("args = %v, want [%%%% %%%%]", args)
	}
}

func TestQueryBuilder_BuildWhereClause_WhitespaceKeyword(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	// Whitespace keyword
	clause, args := qb.BuildWhereClause([]string{"   "}, repository.ArticleSearchFilters{})

	expectedClause := "WHERE (title LIKE ? OR summary LIKE ?)"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	// Whitespace is preserved
	if args[0] != "%   %" {
		t.Errorf("args[0] = %q, want %q", args[0], "%   %")
	}
}

func TestQueryBuilder_BuildWhereClause_ManyKeywords(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	// Test with 5 keywords
	keywords := []string{"golang", "testing", "api", "database", "performance"}
	clause, args := qb.BuildWhereClause(keywords, repository.ArticleSearchFilters{})

	// Should have 5 conditions joined with AND
	expectedClause := "WHERE (title LIKE ? OR summary LIKE ?) AND (title LIKE ? OR summary LIKE ?) AND (title LIKE ? OR summary LIKE ?) AND (title LIKE ? OR summary LIKE ?) AND (title LIKE ? OR summary LIKE ?)"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	// 5 keywords * 2 = 10 args
	if len(args) != 10 {
		t.Fatalf("args length = %d, want 10", len(args))
	}
}

func TestQueryBuilder_BuildWhereClause_NilFilters(t *testing.T) {
	t.Parallel()

	qb := sqlite.NewArticleQueryBuilder()

	// Explicitly nil filters (same as zero value)
	filters := repository.ArticleSearchFilters{
		SourceID: nil,
		From:     nil,
		To:       nil,
	}

	clause, args := qb.BuildWhereClause([]string{"test"}, filters)

	// Should only have keyword condition
	expectedClause := "WHERE (title LIKE ? OR summary LIKE ?)"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}

	if len(args) != 2 {
		t.Fatalf("args length = %d, want 2", len(args))
	}
}
