package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"technical-specification-review-agent/internal/api/http/handlers"
)

type Dependencies struct {
	AnalysisService    handlers.AnalysisService
	GoogleOAuthService handlers.GoogleOAuthService
}

func NewRouter(deps Dependencies) http.Handler {
	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)

	healthHandler := handlers.NewHealthHandler()
	analysisHandler := handlers.NewAnalysisHandler(deps.AnalysisService)
	googleOAuthHandler := handlers.NewGoogleOAuthHandler(deps.GoogleOAuthService)

	router.Get("/health", healthHandler.Handle)
	router.Get("/oauth/google/callback", googleOAuthHandler.Callback)

	router.Route("/api/v1", func(r chi.Router) {
		r.Get("/google/oauth/start", googleOAuthHandler.Start)
		r.Post("/analyses", analysisHandler.Start)
		r.Get("/analyses/{analysisID}", analysisHandler.Get)
		r.Post("/analyses/{analysisID}/publish-comments", analysisHandler.PublishComments)
	})

	return router
}
