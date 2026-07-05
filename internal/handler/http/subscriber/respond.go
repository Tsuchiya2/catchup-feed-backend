package subscriber

import (
	"errors"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	subUC "catchup-feed/internal/usecase/subscriber"
)

// respondUsecaseError maps use case sentinel errors to HTTP statuses:
// not-found → 404, state conflict (deactivated subscriber) → 409,
// validation → 400, anything else → sanitized 500.
func respondUsecaseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, subUC.ErrSubscriberNotFound), errors.Is(err, subUC.ErrTokenNotFound):
		respond.SafeError(w, http.StatusNotFound, err)
	case errors.Is(err, subUC.ErrSubscriberDeactivated):
		respond.SafeError(w, http.StatusConflict, err)
	case errors.Is(err, subUC.ErrNameRequired), errors.Is(err, subUC.ErrInvalidEmail):
		respond.SafeError(w, http.StatusBadRequest, err)
	default:
		respond.SafeError(w, http.StatusInternalServerError, err)
	}
}
