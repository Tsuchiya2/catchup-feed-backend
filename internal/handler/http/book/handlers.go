// Package book exposes the dashboard book-PDF management API (D-25):
// JWT-protected upload / list / delete on the admin listener, plus the
// unauthenticated tailnet-only PDF download the Mac ingest worker uses.
package book

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"catchup-feed/internal/handler/http/respond"
	bookUC "catchup-feed/internal/usecase/book"
)

// maxTitleBytes bounds the multipart title field (display string).
const maxTitleBytes = 1 << 10

type ListHandler struct{ Svc *bookUC.Service }

// ServeHTTP 書籍一覧取得
// @Summary      書籍一覧取得
// @Description  アップロード済み PDF(ディスク)・ingest 済み書籍(books)・book_ingest ジョブを突き合わせた一覧を返します。status は jobs の状態から導出されます(pending=取り込み待ち / processing=Mac worker が処理中 / done=完了 / failed=失敗)。CLI で ingest した書籍(PDF が Pi に無いもの)は size_bytes / uploaded_at が null になります
// @Tags         books
// @Security     BearerAuth
// @Produce      json
// @Success      200 {array} DTO "書籍一覧"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /books [get]
func (h ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	entries, err := h.Svc.List(r.Context())
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	out := make([]DTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, toDTO(e))
	}
	respond.JSON(w, http.StatusOK, out)
}

type UploadHandler struct{ Svc *bookUC.Service }

// ServeHTTP 書籍 PDF アップロード
// @Summary      書籍 PDF アップロード
// @Description  PDF を BOOKS_DIR に保存し、kind='book_ingest' のジョブを投入します(取り込みは Mac の worker が夜間に実行、C-4)。検証: 拡張子 .pdf + マジックバイト %PDF、上限 100MB(D-25)。同名ファイルの再アップロードは置き換え+ジョブ再投入です(冪等。既存の pending ジョブがあれば重複投入せず、そのジョブの payload タイトルを新しいタイトルに更新します)
// @Tags         books
// @Security     BearerAuth
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData file   true  "PDF ファイル"
// @Param        title formData string false "タイトル(省略時はファイル名から)"
// @Success      201 {object} DTO "受理された書籍(status=pending)"
// @Failure      400 {object} respond.ErrorResponse "Bad request - PDF ではない、ファイル名不正、file フィールド欠落"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      413 {object} respond.ErrorResponse "Payload too large - 100MB 超過"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /books [post]
func (h UploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Streamed multipart parsing: the PDF part goes straight to a staged
	// temp file (no 100MB buffering), the title may arrive before or after.
	mr, err := r.MultipartReader()
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, errors.New("invalid multipart/form-data request"))
		return
	}

	var (
		title  string
		staged *bookUC.StagedUpload
	)
	// Discard is nil-safe and a no-op once Commit succeeded. The closure is
	// needed so the deferred call sees the staged value assigned below.
	defer func() { staged.Discard() }()

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			respondUsecaseError(w, err)
			return
		}
		switch part.FormName() {
		case "title":
			raw, err := io.ReadAll(io.LimitReader(part, maxTitleBytes+1))
			if err != nil {
				respondUsecaseError(w, err)
				return
			}
			if len(raw) > maxTitleBytes {
				respond.SafeError(w, http.StatusBadRequest, errors.New("invalid title: too long"))
				return
			}
			title = strings.TrimSpace(string(raw))
		case "file":
			if staged != nil {
				respond.SafeError(w, http.StatusBadRequest, errors.New("invalid request: duplicate file field"))
				return
			}
			staged, err = h.Svc.Stage(part.FileName(), part)
			if err != nil {
				respondUsecaseError(w, err)
				return
			}
		}
		// Unknown fields are skipped; NextPart discards unread part bodies.
	}

	if staged == nil {
		respond.SafeError(w, http.StatusBadRequest, errors.New("invalid request: file field is required"))
		return
	}
	entry, err := h.Svc.Commit(r.Context(), staged, title)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusCreated, toDTO(entry))
}

type DeleteHandler struct{ Svc *bookUC.Service }

// ServeHTTP 書籍削除
// @Summary      書籍削除
// @Description  正準ファイル名をキーに書籍を削除します: books 行(book_chunks・学習項目も含めて削除)+ PDF ファイル + 未処理(pending)の book_ingest ジョブ取消。ingest 前(books 行なし)でもファイルとジョブの削除で成功します。対象は Pi にアップロードされた書籍(GET /books の deletable=true)のみで、CLI 取り込み書籍(deletable=false、file_path が Mac パス)はこの API では削除できません(pulse-books CLI 側で管理)
// @Tags         books
// @Security     BearerAuth
// @Param        filename path string true "正準ファイル名(例: golang-book.pdf)"
// @Success      204 "No Content"
// @Failure      400 {object} respond.ErrorResponse "Bad request - ファイル名不正"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      404 {object} respond.ErrorResponse "Not found - ファイルも books 行も pending ジョブも存在しない"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /books/{filename} [delete]
func (h DeleteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.Delete(r.Context(), r.PathValue("filename")); err != nil {
		respondUsecaseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// respondUsecaseError maps use-case errors to HTTP statuses: validation →
// 400, size ceiling (use-case or http.MaxBytesReader) → 413, not-found →
// 404, anything else → sanitized 500.
func respondUsecaseError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.Is(err, bookUC.ErrNotFound):
		respond.SafeError(w, http.StatusNotFound, err)
	case errors.Is(err, bookUC.ErrTooLarge):
		respond.SafeError(w, http.StatusRequestEntityTooLarge, err)
	case errors.As(err, &maxBytesErr):
		respond.SafeError(w, http.StatusRequestEntityTooLarge, bookUC.ErrTooLarge)
	case errors.Is(err, bookUC.ErrInvalidFilename), errors.Is(err, bookUC.ErrNotPDF):
		respond.SafeError(w, http.StatusBadRequest, err)
	default:
		respond.SafeError(w, http.StatusInternalServerError, err)
	}
}
