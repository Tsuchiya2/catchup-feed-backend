package learning

import (
	"errors"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	learnUC "catchup-feed/internal/usecase/learning"
)

// respondUsecaseError maps learning usecase errors onto HTTP statuses.
// ErrReviewAlreadyGraded is the §8.1 一発確定 409: 採点済み(手動・auto
// とも)・並行採点の全ケースが同じ形で落ちる。
func respondUsecaseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, learnUC.ErrReviewNotFound),
		errors.Is(err, learnUC.ErrItemNotFound),
		errors.Is(err, learnUC.ErrBookNotFound):
		respond.SafeError(w, http.StatusNotFound, err)
	case errors.Is(err, learnUC.ErrReviewAlreadyGraded):
		respond.SafeError(w, http.StatusConflict, err)
	case errors.Is(err, learnUC.ErrInvalidResult),
		errors.Is(err, learnUC.ErrInvalidStatus):
		respond.SafeError(w, http.StatusBadRequest, err)
	default:
		respond.SafeError(w, http.StatusInternalServerError, err)
	}
}
