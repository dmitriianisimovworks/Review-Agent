package app

import (
	"context"
	"net/http"
	"time"

	api "technical-specification-review-agent/internal/api/http"
	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/integration/llm"
	"technical-specification-review-agent/internal/parser"
	"technical-specification-review-agent/internal/repository/memory"
	"technical-specification-review-agent/internal/service"
)

type App struct {
	server *http.Server
}

func New() (*App, error) {
	cfg := config.Load()

	documentParser := parser.NewNoopParser()
	analysisRepo := memory.NewAnalysisRepository()
	documentRepo := memory.NewDocumentRepository()
	llmClient := llm.NewNoopClient()
	docsPublisher := google.NewNoopCommentPublisher()

	analysisService := service.NewAnalysisService(
		documentRepo,
		analysisRepo,
		documentParser,
		llmClient,
		docsPublisher,
	)

	handler := api.NewRouter(api.Dependencies{
		AnalysisService: analysisService,
	})

	server := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{server: server}, nil
}

func (a *App) Run() error {
	return a.server.ListenAndServe()
}

func (a *App) Shutdown(ctx context.Context) error {
	return a.server.Shutdown(ctx)
}
