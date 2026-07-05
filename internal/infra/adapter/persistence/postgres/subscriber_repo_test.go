package postgres_test

import (
	"context"
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

var subscriberCols = []string{"id", "name", "note", "email", "created_at", "deactivated_at"}

func newSubscriberRepo(t *testing.T) (repository.SubscriberRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return pg.NewSubscriberRepo(db), mock, func() { _ = db.Close() }
}

func TestSubscriberRepo_Create(t *testing.T) {
	repo, mock, closeFn := newSubscriberRepo(t)
	defer closeFn()

	note := "配信時間の感想がほしい"
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO subscribers")).
		WithArgs("友人A", &note, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(3), now))

	subscriber := &entity.Subscriber{Name: "友人A", Note: &note}
	require.NoError(t, repo.Create(context.Background(), subscriber))
	assert.Equal(t, int64(3), subscriber.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriberRepo_Get(t *testing.T) {
	now := time.Now()
	deactivated := now.Add(time.Hour)

	tests := []struct {
		name       string
		rows       *sqlmock.Rows
		wantNil    bool
		wantActive bool
	}{
		{
			name: "active subscriber",
			rows: sqlmock.NewRows(subscriberCols).
				AddRow(int64(1), "友人A", nil, "a@example.com", now, nil),
			wantActive: true,
		},
		{
			name: "deactivated subscriber",
			rows: sqlmock.NewRows(subscriberCols).
				AddRow(int64(1), "友人B", nil, nil, now, deactivated),
			wantActive: false,
		},
		{
			name:    "not found returns nil, nil",
			rows:    sqlmock.NewRows(subscriberCols),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newSubscriberRepo(t)
			defer closeFn()

			mock.ExpectQuery(regexp.QuoteMeta("FROM subscribers")).
				WithArgs(int64(1)).
				WillReturnRows(tt.rows)

			got, err := repo.Get(context.Background(), 1)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantActive, got.IsActive())
		})
	}
}

func TestSubscriberRepo_List(t *testing.T) {
	repo, mock, closeFn := newSubscriberRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("FROM subscribers")).
		WillReturnRows(sqlmock.NewRows(subscriberCols).
			AddRow(int64(1), "友人A", nil, nil, now, nil))

	got, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestSubscriberRepo_Update(t *testing.T) {
	repo, mock, closeFn := newSubscriberRepo(t)
	defer closeFn()

	email := "new@example.com"
	mock.ExpectExec("UPDATE subscribers").
		WithArgs("新しい名前", nil, &email, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Update(context.Background(), &entity.Subscriber{
		ID: 1, Name: "新しい名前", Email: &email,
	}))
}

func TestSubscriberRepo_Deactivate(t *testing.T) {
	repo, mock, closeFn := newSubscriberRepo(t)
	defer closeFn()

	at := time.Now()
	mock.ExpectExec(regexp.QuoteMeta("deactivated_at IS NULL")).
		WithArgs(at, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Deactivate(context.Background(), 1, at))
	assert.NoError(t, mock.ExpectationsWereMet())
}
