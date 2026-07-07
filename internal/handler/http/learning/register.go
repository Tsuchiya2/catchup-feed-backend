package learning

import (
	"net/http"

	"catchup-feed/internal/handler/http/auth"
	learnUC "catchup-feed/internal/usecase/learning"
)

// Register registers the §8.1 learning admin routes (C-21 flat 構成).
// Every route is wrapped in auth.Authz: 理解状態(stage・採点履歴)は
// 私的データであり、JWT の内側にしか出ない(§10)。The explicit wrap keeps
// that true even if the mux is ever mounted without the outer Authz —
// same stance as the subscriber routes.
//
// 集計エンドポイント(オーバーデュー件数・ストリーク等)は追加しない
// こと(§2 Out: 罪悪感 UI 禁止)。pending が空は正常系(200 + 空配列)。
func Register(mux *http.ServeMux, svc learnUC.Service) {
	mux.Handle("GET /learning/reviews/pending", auth.Authz(PendingReviewsHandler{svc}))
	mux.Handle("POST /learning/reviews/{id}/grade", auth.Authz(GradeHandler{svc}))

	mux.Handle("GET /learning/items", auth.Authz(ListItemsHandler{svc}))
	mux.Handle("POST /learning/items/{id}/retire", auth.Authz(RetireItemHandler{svc}))

	mux.Handle("GET /learning/books", auth.Authz(ListBooksHandler{svc}))
	mux.Handle("POST /learning/books/{id}/activate", auth.Authz(ActivateBookHandler{svc}))
	mux.Handle("POST /learning/books/{id}/deactivate", auth.Authz(DeactivateBookHandler{svc}))
}
