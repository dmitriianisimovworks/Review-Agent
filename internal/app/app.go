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
	platformpostgres "technical-specification-review-agent/internal/platform/postgres"
	platformredis "technical-specification-review-agent/internal/platform/redis"
	"technical-specification-review-agent/internal/prompt"
	postgresrepo "technical-specification-review-agent/internal/repository/postgres"
	redisrepo "technical-specification-review-agent/internal/repository/redis"
	"technical-specification-review-agent/internal/service"
)

type App struct {
	server *http.Server
}

func New() (*App, error) {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pgPool, err := platformpostgres.NewPool(ctx, cfg.DB.URL)
	if err != nil {
		return nil, err
	}

	if _, err := pgPool.Exec(ctx, postgresrepo.SchemaSQL); err != nil {
		return nil, err
	}

	redisClient, err := platformredis.NewClient(ctx, cfg.Redis.URL)
	if err != nil {
		return nil, err
	}

	documentParser := parser.NewChunkingParser(cfg.Document)
	documentRepo := postgresrepo.NewDocumentRepository(pgPool)
	analysisRepo := postgresrepo.NewAnalysisRepository(pgPool)
	analysisCache := redisrepo.NewAnalysisCache(redisClient, 10*time.Minute)
	promptBuilder := prompt.NewDefaultBuilder()
	llmClient := llm.NewOpenAICompatibleClient(cfg.LLM, promptBuilder)
	docsPublisher := google.NewNoopCommentPublisher()

	analysisService := service.NewAnalysisService(
		documentRepo,
		analysisRepo,
		analysisCache,
		documentParser,
		llmClient,
		docsPublisher,
		cfg.LLM.Provider,
		cfg.LLM.Model,
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
