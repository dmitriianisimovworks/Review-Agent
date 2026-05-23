package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

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
	server      *http.Server
	pgPool      *pgxpool.Pool
	redisClient *goredis.Client
}

func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pgPool, err := platformpostgres.NewPool(ctx, cfg.DB.URL)
	if err != nil {
		return nil, err
	}

	if err := platformpostgres.RunMigrations(ctx, pgPool); err != nil {
		pgPool.Close()
		return nil, err
	}

	redisClient, err := platformredis.NewClient(ctx, cfg.Redis.URL)
	if err != nil {
		pgPool.Close()
		return nil, err
	}

	documentParser := parser.NewChunkingParser(cfg.Document)
	documentRepo := postgresrepo.NewDocumentRepository(pgPool)
	analysisRepo := postgresrepo.NewAnalysisRepository(pgPool)
	analysisCache := redisrepo.NewAnalysisCache(redisClient, time.Duration(cfg.Cache.AnalysisTTLSeconds)*time.Second)
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

	return &App{
		server:      server,
		pgPool:      pgPool,
		redisClient: redisClient,
	}, nil
}

func (a *App) Run() error {
	err := a.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a *App) Shutdown(ctx context.Context) error {
	var shutdownErr error
	if err := a.server.Shutdown(ctx); err != nil {
		shutdownErr = err
	}
	if a.redisClient != nil {
		_ = a.redisClient.Close()
	}
	if a.pgPool != nil {
		a.pgPool.Close()
	}
	return shutdownErr
}
