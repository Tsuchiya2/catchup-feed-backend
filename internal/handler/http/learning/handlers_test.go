package learning_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hlearning "catchup-feed/internal/handler/http/learning"
	learncore "catchup-feed/internal/learning"
	"catchup-feed/internal/repository"
	learnUC "catchup-feed/internal/usecase/learning"
)

// fakeRepo drives the handlers through the real usecase Service, the same
// composition style as the source handler tests.
type fakeRepo struct {
	pending    []repository.PendingReview
	pendingErr error

	gradeOutcome repository.GradeOutcome
	gradeErr     error

	items       []repository.LearningItemSummary
	itemsErr    error
	lastRetired *bool

	retiredAt time.Time
	retireErr error

	books    []repository.ReviewBook
	booksErr error

	book          repository.ReviewBook
	activateErr   error
	deactivateErr error
}

func (f *fakeRepo) ListPendingReviews(context.Context) ([]repository.PendingReview, error) {
	return f.pending, f.pendingErr
}

func (f *fakeRepo) GradeReview(_ context.Context, _ int64, _ string, _ time.Time, _ []int) (repository.GradeOutcome, error) {
	return f.gradeOutcome, f.gradeErr
}

func (f *fakeRepo) ListItems(_ context.Context, retired bool) ([]repository.LearningItemSummary, error) {
	f.lastRetired = &retired
	return f.items, f.itemsErr
}

func (f *fakeRepo) RetireItem(context.Context, int64) (time.Time, error) {
	return f.retiredAt, f.retireErr
}

func (f *fakeRepo) ListBooks(context.Context) ([]repository.ReviewBook, error) {
	return f.books, f.booksErr
}

func (f *fakeRepo) ActivateBook(context.Context, int64) (repository.ReviewBook, error) {
	return f.book, f.activateErr
}

func (f *fakeRepo) DeactivateBook(context.Context, int64) (repository.ReviewBook, error) {
	return f.book, f.deactivateErr
}

func newSvc(repo *fakeRepo) learnUC.Service {
	return learnUC.Service{
		Repo:   repo,
		Ladder: []int{1, 7, 30},
		Now:    func() time.Time { return time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC) },
	}
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestPendingReviewsHandler(t *testing.T) {
	t.Run("returns the grading queue with formatted asked_on", func(t *testing.T) {
		repo := &fakeRepo{pending: []repository.PendingReview{
			{LogID: 12, ItemID: 3, AskedOn: day(2026, 7, 6), Concept: "c1", Question: "q1", Answer: "a1"},
			{LogID: 15, ItemID: 4, AskedOn: day(2026, 7, 7), Concept: "c2", Question: "q2", Answer: "a2"},
		}}
		w := httptest.NewRecorder()
		hlearning.PendingReviewsHandler{Svc: newSvc(repo)}.ServeHTTP(w,
			httptest.NewRequest(http.MethodGet, "/learning/reviews/pending", nil))

		require.Equal(t, http.StatusOK, w.Code)
		var got []hlearning.PendingReviewDTO
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Len(t, got, 2)
		assert.Equal(t, hlearning.PendingReviewDTO{
			LogID: 12, ItemID: 3, AskedOn: "2026-07-06", Concept: "c1", Question: "q1", Answer: "a1",
		}, got[0])
	})

	// §8.2: 「今日は採点するものがありません」は正常系 — 200 と JSON の
	// 空配列(null ではなく [])。
	t.Run("empty queue is 200 with an empty array", func(t *testing.T) {
		w := httptest.NewRecorder()
		hlearning.PendingReviewsHandler{Svc: newSvc(&fakeRepo{})}.ServeHTTP(w,
			httptest.NewRequest(http.MethodGet, "/learning/reviews/pending", nil))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "[]", strings.TrimSpace(w.Body.String()))
	})
}

func TestGradeHandler(t *testing.T) {
	okOutcome := repository.GradeOutcome{ItemID: 3, Stage: 1, DueOn: day(2026, 7, 14), Retired: false}
	tests := []struct {
		name     string
		pathID   string
		body     string
		repo     *fakeRepo
		wantCode int
	}{
		{
			name:   "grade applies and returns the transitioned state",
			pathID: "12", body: `{"result":"good"}`,
			repo: &fakeRepo{gradeOutcome: okOutcome}, wantCode: http.StatusOK,
		},
		{
			name:   "non-numeric id is 400",
			pathID: "abc", body: `{"result":"good"}`,
			repo: &fakeRepo{}, wantCode: http.StatusBadRequest,
		},
		{
			name:   "malformed body is 400",
			pathID: "12", body: `{`,
			repo: &fakeRepo{}, wantCode: http.StatusBadRequest,
		},
		{
			name:   "invalid result is 400",
			pathID: "12", body: `{"result":"auto"}`,
			repo: &fakeRepo{}, wantCode: http.StatusBadRequest,
		},
		{
			name:   "absent log is 404",
			pathID: "12", body: `{"result":"good"}`,
			repo: &fakeRepo{gradeErr: repository.ErrReviewLogNotFound}, wantCode: http.StatusNotFound,
		},
		{
			// 一発確定(§8.1): 手動採点済み・auto 解決済み・並行採点の
			// 全ケースが同じ 409 に落ちる。
			name:   "already graded is 409",
			pathID: "12", body: `{"result":"good"}`,
			repo: &fakeRepo{gradeErr: repository.ErrReviewLogGraded}, wantCode: http.StatusConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/learning/reviews/"+tt.pathID+"/grade",
				strings.NewReader(tt.body))
			r.SetPathValue("id", tt.pathID)
			w := httptest.NewRecorder()
			hlearning.GradeHandler{Svc: newSvc(tt.repo)}.ServeHTTP(w, r)

			require.Equal(t, tt.wantCode, w.Code)
			if tt.wantCode == http.StatusOK {
				var got hlearning.GradeResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
				assert.Equal(t, hlearning.GradeResponse{
					LogID: 12, ItemID: 3, Result: "good", Stage: 1, DueOn: "2026-07-14", Retired: false,
				}, got)
			} else {
				var body map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.NotEmpty(t, body["error"], "errors use the respond.ErrorResponse shape")
			}
		})
	}
}

func TestListItemsHandler(t *testing.T) {
	t.Run("maps summaries and defaults to active", func(t *testing.T) {
		articleID := int64(42)
		lastResult := "good"
		lastAsked := day(2026, 7, 7)
		repo := &fakeRepo{items: []repository.LearningItemSummary{{
			Item:       itemFixture(3, &articleID),
			TimesAsked: 2, LastResult: &lastResult, LastAskedOn: &lastAsked,
		}}}
		w := httptest.NewRecorder()
		hlearning.ListItemsHandler{Svc: newSvc(repo)}.ServeHTTP(w,
			httptest.NewRequest(http.MethodGet, "/learning/items", nil))

		require.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, repo.lastRetired)
		assert.False(t, *repo.lastRetired, "no ?status defaults to active")

		var got []hlearning.ItemDTO
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Len(t, got, 1)
		assert.Equal(t, "2026-07-14", got[0].DueOn)
		assert.Equal(t, 2, got[0].TimesAsked)
		require.NotNil(t, got[0].LastAskedOn)
		assert.Equal(t, "2026-07-07", *got[0].LastAskedOn)
		require.NotNil(t, got[0].LastResult)
		assert.Equal(t, "good", *got[0].LastResult)
	})

	t.Run("status=retired is passed through", func(t *testing.T) {
		repo := &fakeRepo{}
		w := httptest.NewRecorder()
		hlearning.ListItemsHandler{Svc: newSvc(repo)}.ServeHTTP(w,
			httptest.NewRequest(http.MethodGet, "/learning/items?status=retired", nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, repo.lastRetired)
		assert.True(t, *repo.lastRetired)
	})

	t.Run("unknown status is 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		hlearning.ListItemsHandler{Svc: newSvc(&fakeRepo{})}.ServeHTTP(w,
			httptest.NewRequest(http.MethodGet, "/learning/items?status=overdue", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestRetireItemHandler(t *testing.T) {
	t.Run("retire returns the archived state", func(t *testing.T) {
		retiredAt := time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC)
		r := httptest.NewRequest(http.MethodPost, "/learning/items/3/retire", nil)
		r.SetPathValue("id", "3")
		w := httptest.NewRecorder()
		hlearning.RetireItemHandler{Svc: newSvc(&fakeRepo{retiredAt: retiredAt})}.ServeHTTP(w, r)

		require.Equal(t, http.StatusOK, w.Code)
		var got hlearning.RetireResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, hlearning.RetireResponse{ID: 3, RetiredAt: retiredAt}, got)
	})

	t.Run("absent item is 404", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/learning/items/3/retire", nil)
		r.SetPathValue("id", "3")
		w := httptest.NewRecorder()
		hlearning.RetireItemHandler{Svc: newSvc(&fakeRepo{retireErr: repository.ErrLearningItemNotFound})}.ServeHTTP(w, r)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestBookHandlers(t *testing.T) {
	book := repository.ReviewBook{ID: 7, Title: "リーダブルコード", ReviewStatus: "active", ReviewCursor: 12, TotalChunks: 180}
	wantDTO := hlearning.BookDTO{ID: 7, Title: "リーダブルコード", ReviewStatus: "active", ReviewCursor: 12, TotalChunks: 180}

	t.Run("list", func(t *testing.T) {
		w := httptest.NewRecorder()
		hlearning.ListBooksHandler{Svc: newSvc(&fakeRepo{books: []repository.ReviewBook{book}})}.ServeHTTP(w,
			httptest.NewRequest(http.MethodGet, "/learning/books", nil))
		require.Equal(t, http.StatusOK, w.Code)
		var got []hlearning.BookDTO
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, []hlearning.BookDTO{wantDTO}, got)
	})

	t.Run("activate", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/learning/books/7/activate", nil)
		r.SetPathValue("id", "7")
		w := httptest.NewRecorder()
		hlearning.ActivateBookHandler{Svc: newSvc(&fakeRepo{book: book})}.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		var got hlearning.BookDTO
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, wantDTO, got)
	})

	t.Run("activate absent book is 404", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/learning/books/999/activate", nil)
		r.SetPathValue("id", "999")
		w := httptest.NewRecorder()
		hlearning.ActivateBookHandler{Svc: newSvc(&fakeRepo{activateErr: repository.ErrBookNotFound})}.ServeHTTP(w, r)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("deactivate", func(t *testing.T) {
		idle := book
		idle.ReviewStatus = "idle"
		r := httptest.NewRequest(http.MethodPost, "/learning/books/7/deactivate", nil)
		r.SetPathValue("id", "7")
		w := httptest.NewRecorder()
		hlearning.DeactivateBookHandler{Svc: newSvc(&fakeRepo{book: idle})}.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code)
		var got hlearning.BookDTO
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, "idle", got.ReviewStatus)
		assert.Equal(t, 12, got.ReviewCursor, "deactivate keeps the cursor (D-20 一時停止)")
	})

	t.Run("invalid path id is 400", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/learning/books/x/activate", nil)
		r.SetPathValue("id", "x")
		w := httptest.NewRecorder()
		hlearning.ActivateBookHandler{Svc: newSvc(&fakeRepo{})}.ServeHTTP(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestRegister_RequiresJWT pins §10: every /learning route — reads
// included — is wrapped in auth.Authz. 理解状態は JWT の内側にしか出ない。
func TestRegister_RequiresJWT(t *testing.T) {
	mux := http.NewServeMux()
	hlearning.Register(mux, newSvc(&fakeRepo{}))

	routes := []struct{ method, path string }{
		{http.MethodGet, "/learning/reviews/pending"},
		{http.MethodPost, "/learning/reviews/12/grade"},
		{http.MethodGet, "/learning/items"},
		{http.MethodPost, "/learning/items/3/retire"},
		{http.MethodGet, "/learning/books"},
		{http.MethodPost, "/learning/books/7/activate"},
		{http.MethodPost, "/learning/books/7/deactivate"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(rt.method, rt.path, strings.NewReader(`{"result":"good"}`)))
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"a request without a JWT must not reach the handler")
		})
	}
}

func itemFixture(id int64, articleID *int64) learncore.Item {
	return learncore.Item{
		ID: id, Kind: "article", ArticleID: articleID,
		Concept: "c", Question: "q", Answer: "a", Provider: "gemini",
		Stage: 1, DueOn: day(2026, 7, 14),
		CreatedAt: time.Date(2026, 7, 6, 19, 30, 0, 0, time.UTC),
	}
}
