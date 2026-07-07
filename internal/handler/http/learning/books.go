package learning

import (
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	learnUC "catchup-feed/internal/usecase/learning"
)

type ListBooksHandler struct{ Svc learnUC.Service }

// ServeHTTP 書籍一覧取得
// @Summary      書籍一覧取得(book review 管理)
// @Description  ingest 済み書籍の一覧を返します(D-20)。review_status(idle=未対象/一時停止、active=進行中・常に最大1冊、finished=読了)と review_cursor / total_chunks(進捗率の素材)を含みます
// @Tags         learning
// @Security     BearerAuth
// @Produce      json
// @Success      200 {array} BookDTO "書籍一覧"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /learning/books [get]
func (h ListBooksHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	books, err := h.Svc.ListBooks(r.Context())
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	out := make([]BookDTO, 0, len(books))
	for _, b := range books {
		out = append(out, toBookDTO(b))
	}
	respond.JSON(w, http.StatusOK, out)
}

type ActivateBookHandler struct{ Svc learnUC.Service }

// ServeHTTP 書籍を進行中に指定
// @Summary      書籍を進行中に指定
// @Description  書籍を review_status='active'(book_review コーナーの対象、§7.3)にします。既存の active 書籍があれば同一トランザクションで idle に落とします — 入れ替えが1操作で完結し、active は常に最大1冊です(D-20)。finished の書籍にも実行できます(再読。カーソルは動かしません)。冪等
// @Tags         learning
// @Security     BearerAuth
// @Produce      json
// @Param        id path int true "書籍ID"
// @Success      200 {object} BookDTO "進行中になった書籍"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid ID"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      404 {object} respond.ErrorResponse "Not found - book not found"
// @Router       /learning/books/{id}/activate [post]
func (h ActivateBookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	book, err := h.Svc.ActivateBook(r.Context(), id)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, toBookDTO(book))
}

type DeactivateBookHandler struct{ Svc learnUC.Service }

// ServeHTTP 書籍の一時停止
// @Summary      書籍の一時停止/対象から除外
// @Description  進行中の書籍を review_status='idle' に戻します。review_cursor は保持されるため、再度 activate すると続きから再開します(D-20)。冪等: idle の書籍への再実行は 200、finished の書籍は読了マーカーを保持したまま何もしません
// @Tags         learning
// @Security     BearerAuth
// @Produce      json
// @Param        id path int true "書籍ID"
// @Success      200 {object} BookDTO "現在の書籍状態"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid ID"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      404 {object} respond.ErrorResponse "Not found - book not found"
// @Router       /learning/books/{id}/deactivate [post]
func (h DeactivateBookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	book, err := h.Svc.DeactivateBook(r.Context(), id)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, toBookDTO(book))
}
