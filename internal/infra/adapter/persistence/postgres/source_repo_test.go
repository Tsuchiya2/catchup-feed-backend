package postgres_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	pg "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/repository"
)

/* ─────────────────────────── ヘルパ ─────────────────────────── */

// sourceCols is the §4 sources column list (+ Phase 2 kind).
var sourceCols = []string{
	"id", "name", "feed_url", "category", "lang", "kind", "active", "created_at",
}

func srcRow(s *entity.Source) *sqlmock.Rows {
	return sqlmock.NewRows(sourceCols).AddRow(
		s.ID, s.Name, s.FeedURL, s.Category, s.Lang, s.Kind, s.Active, s.CreatedAt,
	)
}

func newSourceRepo(t *testing.T) (repository.SourceRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return pg.NewSourceRepo(db), mock, func() { _ = db.Close() }
}

/* ─────────────────────────── Get ─────────────────────────── */

func TestSourceRepo_Get(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		rows    *sqlmock.Rows
		queryEr error
		want    *entity.Source
		wantErr bool
	}{
		{
			name: "found",
			want: &entity.Source{
				ID: 1, Name: "Golang Weekly",
				FeedURL:  "https://example.com/feed.xml",
				Category: "dev", Lang: "en", Kind: "rss", Active: true, CreatedAt: now,
			},
		},
		{
			name: "not found returns nil, nil",
			rows: sqlmock.NewRows(sourceCols),
		},
		{
			name:    "database error",
			queryEr: errors.New("db down"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newSourceRepo(t)
			defer closeFn()

			exp := mock.ExpectQuery(regexp.QuoteMeta("FROM sources")).
				WithArgs(int64(1))
			switch {
			case tt.queryEr != nil:
				exp.WillReturnError(tt.queryEr)
			case tt.rows != nil:
				exp.WillReturnRows(tt.rows)
			default:
				exp.WillReturnRows(srcRow(tt.want))
			}

			got, err := repo.Get(context.Background(), 1)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

/* ─────────────────────────── List / ListActive / Search ─────────────────────────── */

func TestSourceRepo_List(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectQuery("FROM sources").
		WillReturnRows(srcRow(&entity.Source{
			ID: 1, Name: "n", FeedURL: "u", Category: "dev", Lang: "en",
			Kind: "rss", Active: true, CreatedAt: now,
		}))

	got, err := repo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "dev", got[0].Category)
	assert.Equal(t, "en", got[0].Lang)
}

func TestSourceRepo_ListActive(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	mock.ExpectQuery("WHERE active = TRUE").
		WillReturnRows(sqlmock.NewRows(sourceCols))

	got, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSourceRepo_List_ScanError(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	mock.ExpectQuery("FROM sources").
		WillReturnRows(sqlmock.NewRows(sourceCols).
			AddRow("not-an-int", "n", "u", "dev", "en", "rss", true, time.Now()))

	_, err := repo.List(context.Background())
	assert.Error(t, err)
}

func TestSourceRepo_Search(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	mock.ExpectQuery("ILIKE").
		WithArgs("%go%").
		WillReturnRows(sqlmock.NewRows(sourceCols))

	_, err := repo.Search(context.Background(), "go")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

/* ─────────────────────────── SearchWithFilters ─────────────────────────── */

func TestSourceRepo_SearchWithFilters(t *testing.T) {
	category := "ai"
	active := true

	tests := []struct {
		name      string
		keywords  []string
		filters   repository.SourceSearchFilters
		wantQuery string
		wantArgs  []driver.Value
	}{
		{
			name:      "keyword only",
			keywords:  []string{"go"},
			wantQuery: `(name ILIKE $1 OR feed_url ILIKE $1)`,
			wantArgs:  []driver.Value{"%go%"},
		},
		{
			name:      "category filter",
			filters:   repository.SourceSearchFilters{Category: &category},
			wantQuery: `category = $1`,
			wantArgs:  []driver.Value{category},
		},
		{
			name:      "keyword + category + active",
			keywords:  []string{"go"},
			filters:   repository.SourceSearchFilters{Category: &category, Active: &active},
			wantQuery: `active = $3`,
			wantArgs:  []driver.Value{"%go%", category, active},
		},
		{
			name:      "no criteria returns browse-mode query",
			wantQuery: `ORDER BY id ASC`,
			wantArgs:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newSourceRepo(t)
			defer closeFn()

			mock.ExpectQuery(regexp.QuoteMeta(tt.wantQuery)).
				WithArgs(tt.wantArgs...).
				WillReturnRows(sqlmock.NewRows(sourceCols))

			got, err := repo.SearchWithFilters(context.Background(), tt.keywords, tt.filters)
			require.NoError(t, err)
			assert.Empty(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSourceRepo_SearchWithFilters_EscapesILIKE(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	// % / _ / \ must arrive escaped in the bind parameter.
	mock.ExpectQuery("ILIKE").
		WithArgs(`%50\%\_off\\%`).
		WillReturnRows(sqlmock.NewRows(sourceCols))

	_, err := repo.SearchWithFilters(context.Background(), []string{`50%_off\`}, repository.SourceSearchFilters{})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

/* ─────────────────────────── Create / Update / Delete ─────────────────────────── */

func TestSourceRepo_Create(t *testing.T) {
	tests := []struct {
		name     string
		source   *entity.Source
		wantLang string
		wantKind string
	}{
		{
			name: "explicit lang",
			source: &entity.Source{
				Name: "Publickey", FeedURL: "https://example.com/atom.xml",
				Category: "community", Lang: "ja", Kind: "rss", Active: true,
			},
			wantLang: "ja",
			wantKind: "rss",
		},
		{
			name: "empty lang and kind default to en / rss",
			source: &entity.Source{
				Name: "Golang Weekly", FeedURL: "https://example.com/feed.xml",
				Category: "dev", Active: true,
			},
			wantLang: entity.DefaultSourceLang,
			wantKind: entity.DefaultSourceKind,
		},
		{
			name: "podcast kind is persisted",
			source: &entity.Source{
				Name: "fukabori.fm", FeedURL: "https://example.com/podcast.rss",
				Category: "dev", Lang: "ja", Kind: entity.SourceKindPodcast, Active: true,
			},
			wantLang: "ja",
			wantKind: entity.SourceKindPodcast,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newSourceRepo(t)
			defer closeFn()

			now := time.Now()
			mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO sources")).
				WithArgs(tt.source.Name, tt.source.FeedURL, tt.source.Category, tt.wantLang, tt.wantKind, true).
				WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(5), now))

			err := repo.Create(context.Background(), tt.source)
			require.NoError(t, err)
			assert.Equal(t, int64(5), tt.source.ID)
			assert.Equal(t, tt.wantLang, tt.source.Lang)
			assert.Equal(t, tt.wantKind, tt.source.Kind)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSourceRepo_Create_DatabaseError(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO sources")).
		WillReturnError(errors.New("duplicate key"))

	err := repo.Create(context.Background(), &entity.Source{
		Name: "n", FeedURL: "u", Category: "dev",
	})
	assert.Error(t, err)
}

func TestSourceRepo_Update(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	mock.ExpectExec("UPDATE sources").
		WithArgs("new", "https://u", "ai", "en", "youtube", false, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Update(context.Background(), &entity.Source{
		ID: 1, Name: "new", FeedURL: "https://u",
		Category: "ai", Lang: "en", Kind: "youtube", Active: false,
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSourceRepo_Update_NoRowsAffected(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	mock.ExpectExec("UPDATE sources").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Update(context.Background(), &entity.Source{
		ID: 99, Name: "n", FeedURL: "u", Category: "dev",
	})
	assert.Error(t, err)
}

func TestSourceRepo_Delete(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM sources WHERE id = $1")).
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Delete(context.Background(), 1))
}

func TestSourceRepo_Delete_NoRowsAffected(t *testing.T) {
	repo, mock, closeFn := newSourceRepo(t)
	defer closeFn()

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM sources WHERE id = $1")).
		WithArgs(int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	assert.Error(t, repo.Delete(context.Background(), 99))
}
