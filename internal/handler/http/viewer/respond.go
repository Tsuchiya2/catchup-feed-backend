package viewer

import (
	"errors"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	viewerUC "catchup-feed/internal/usecase/viewer"
)

// respondUsecaseError maps use case sentinel errors to HTTP statuses:
// not-found → 404, email collision → 409, validation → 400, anything else
// → sanitized 500.
func respondUsecaseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, viewerUC.ErrViewerNotFound):
		respond.SafeError(w, http.StatusNotFound, err)
	case errors.Is(err, viewerUC.ErrEmailTaken):
		respond.SafeError(w, http.StatusConflict, err)
	case errors.Is(err, viewerUC.ErrNameRequired),
		errors.Is(err, viewerUC.ErrInvalidEmail),
		errors.Is(err, viewerUC.ErrPasswordTooShort),
		errors.Is(err, viewerUC.ErrPasswordTooLong):
		respond.SafeError(w, http.StatusBadRequest, err)
	default:
		respond.SafeError(w, http.StatusInternalServerError, err)
	}
}
