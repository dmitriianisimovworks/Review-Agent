package handlers

import (
	"net/http"

	"technical-specification-review-agent/internal/apperrors"
)

func writeError(w http.ResponseWriter, err error) {
	status := statusFromError(err)
	writeJSON(w, status, map[string]string{
		"error": apperrors.PublicMessage(err),
	})
}

func statusFromError(err error) int {
	switch apperrors.KindOf(err) {
	case apperrors.KindInvalidArgument:
		return http.StatusBadRequest
	case apperrors.KindNotFound:
		return http.StatusNotFound
	case apperrors.KindConflict:
		return http.StatusConflict
	case apperrors.KindDependency:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
