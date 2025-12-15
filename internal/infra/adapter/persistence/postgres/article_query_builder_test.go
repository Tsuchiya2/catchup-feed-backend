package postgres_test

import (
	"testing"
	"time"

	"catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/repository"
)

/* ──────────────────────────── BuildWhereClause Tests ──────────────────────────── */

func TestArticleQueryBuilder_BuildWhereClause_NoConditions(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	clause, args := builder.BuildWhereClause([]string{}, repository.ArticleSearchFilters{}, "")

	if clause != "" {
		t.Errorf("clause should be empty, got %q", clause)
	}
	if len(args) != 0 {
		t.Errorf("args should be empty, got %v", args)
	}
}

func TestArticleQueryBuilder_BuildWhereClause_SingleKeyword(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	clause, args := builder.BuildWhereClause([]string{"Go"}, repository.ArticleSearchFilters{}, "")

	expectedClause := "WHERE (title ILIKE $1 OR summary ILIKE $1)"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}
	if len(args) != 1 {
		t.Fatalf("len(args) = %d, want 1", len(args))
	}
	if args[0] != "%Go%" {
		t.Errorf("args[0] = %q, want %q", args[0], "%Go%")
	}
}

func TestArticleQueryBuilder_BuildWhereClause_MultipleKeywords(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	clause, args := builder.BuildWhereClause([]string{"Go", "release"}, repository.ArticleSearchFilters{}, "")

	expectedClause := "WHERE (title ILIKE $1 OR summary ILIKE $1) AND (title ILIKE $2 OR summary ILIKE $2)"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}
	if len(args) != 2 {
		t.Fatalf("len(args) = %d, want 2", len(args))
	}
	if args[0] != "%Go%" || args[1] != "%release%" {
		t.Errorf("args = %v, want [%%Go%%, %%release%%]", args)
	}
}

func TestArticleQueryBuilder_BuildWhereClause_WithTableAlias(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	clause, args := builder.BuildWhereClause([]string{"Go"}, repository.ArticleSearchFilters{}, "a")

	expectedClause := "WHERE (a.title ILIKE $1 OR a.summary ILIKE $1)"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}
	if len(args) != 1 {
		t.Fatalf("len(args) = %d, want 1", len(args))
	}
}

func TestArticleQueryBuilder_BuildWhereClause_WithSourceIDFilter(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	sourceID := int64(2)
	filters := repository.ArticleSearchFilters{SourceID: &sourceID}
	clause, args := builder.BuildWhereClause([]string{"Go"}, filters, "")

	expectedClause := "WHERE (title ILIKE $1 OR summary ILIKE $1) AND source_id = $2"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}
	if len(args) != 2 {
		t.Fatalf("len(args) = %d, want 2", len(args))
	}
	if args[1] != int64(2) {
		t.Errorf("args[1] = %v, want 2", args[1])
	}
}

func TestArticleQueryBuilder_BuildWhereClause_WithDateFilters(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	filters := repository.ArticleSearchFilters{From: &from, To: &to}
	clause, args := builder.BuildWhereClause([]string{"Go"}, filters, "")

	expectedClause := "WHERE (title ILIKE $1 OR summary ILIKE $1) AND published_at >= $2 AND published_at <= $3"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}
	if len(args) != 3 {
		t.Fatalf("len(args) = %d, want 3", len(args))
	}
}

func TestArticleQueryBuilder_BuildWhereClause_WithAllFilters(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	sourceID := int64(2)
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	filters := repository.ArticleSearchFilters{
		SourceID: &sourceID,
		From:     &from,
		To:       &to,
	}
	clause, args := builder.BuildWhereClause([]string{"Go", "release"}, filters, "a")

	expectedClause := "WHERE (a.title ILIKE $1 OR a.summary ILIKE $1) AND (a.title ILIKE $2 OR a.summary ILIKE $2) AND a.source_id = $3 AND a.published_at >= $4 AND a.published_at <= $5"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}
	if len(args) != 5 {
		t.Fatalf("len(args) = %d, want 5", len(args))
	}
}

func TestArticleQueryBuilder_BuildWhereClause_FiltersOnly(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	sourceID := int64(2)
	filters := repository.ArticleSearchFilters{SourceID: &sourceID}
	clause, args := builder.BuildWhereClause([]string{}, filters, "")

	expectedClause := "WHERE source_id = $1"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}
	if len(args) != 1 {
		t.Fatalf("len(args) = %d, want 1", len(args))
	}
}

func TestArticleQueryBuilder_BuildWhereClause_SpecialCharactersEscaped(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	_, args := builder.BuildWhereClause([]string{"100%", "my_var", "path\\file"}, repository.ArticleSearchFilters{}, "")

	if len(args) != 3 {
		t.Fatalf("len(args) = %d, want 3", len(args))
	}
	// EscapeILIKE should escape special characters
	if args[0] != "%100\\%%" {
		t.Errorf("args[0] = %q, want %%100\\%%%%", args[0])
	}
	if args[1] != "%my\\_var%" {
		t.Errorf("args[1] = %q, want %%my\\_var%%", args[1])
	}
	if args[2] != "%path\\\\file%" {
		t.Errorf("args[2] = %q, want %%path\\\\file%%", args[2])
	}
}

func TestArticleQueryBuilder_BuildWhereClause_OnlyFromFilter(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	filters := repository.ArticleSearchFilters{From: &from}
	clause, args := builder.BuildWhereClause([]string{}, filters, "")

	expectedClause := "WHERE published_at >= $1"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}
	if len(args) != 1 {
		t.Fatalf("len(args) = %d, want 1", len(args))
	}
}

func TestArticleQueryBuilder_BuildWhereClause_OnlyToFilter(t *testing.T) {
	builder := postgres.NewArticleQueryBuilder()
	to := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	filters := repository.ArticleSearchFilters{To: &to}
	clause, args := builder.BuildWhereClause([]string{}, filters, "")

	expectedClause := "WHERE published_at <= $1"
	if clause != expectedClause {
		t.Errorf("clause = %q, want %q", clause, expectedClause)
	}
	if len(args) != 1 {
		t.Fatalf("len(args) = %d, want 1", len(args))
	}
}
