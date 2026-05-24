package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	api "technical-specification-review-agent/internal/api/http"
	"technical-specification-review-agent/internal/comment"
	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
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

type analysisFacade struct {
	analysisService *service.AnalysisService
	commentService  *service.CommentService
}

func (f *analysisFacade) StartAnalysis(ctx context.Context, input service.StartAnalysisInput) (domain.Analysis, error) {
	return f.analysisService.StartAnalysis(ctx, input)
}

func (f *analysisFacade) GetAnalysis(ctx context.Context, id string) (domain.Analysis, error) {
	return f.analysisService.GetAnalysis(ctx, id)
}

func (f *analysisFacade) PublishComments(ctx context.Context, input service.PublishCommentsInput) (service.PublishCommentsResult, error) {
	return f.commentService.PublishComments(ctx, input)
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
	googleOAuthRepo := postgresrepo.NewGoogleOAuthConnectionRepository(pgPool)
	analysisCache := redisrepo.NewAnalysisCache(redisClient, time.Duration(cfg.Cache.AnalysisTTLSeconds)*time.Second)
	promptBuilder := prompt.NewDefaultBuilder()
	commentFormatter := comment.NewDefaultFormatter()
	llmClient := llm.NewOpenAICompatibleClient(cfg.LLM, promptBuilder)
	googleOAuthProvider := google.NewOAuthService(cfg.Google)
	documentReader, err := google.NewServiceAccountReader(ctx, cfg.Google.ServiceAccountFile)
	if err != nil {
		redisClient.Close()
		pgPool.Close()
		return nil, err
	}
	docsPublisher, err := google.NewDriveCommentPublisher(ctx, cfg.Google.ServiceAccountFile)
	if err != nil {
		redisClient.Close()
		pgPool.Close()
		return nil, err
	}

	analysisService := service.NewAnalysisService(
		documentRepo,
		analysisRepo,
		analysisCache,
		documentParser,
		llmClient,
		documentReader,
		docsPublisher,
		cfg.LLM.Provider,
		cfg.LLM.Model,
	)
	commentService := service.NewCommentService(
		documentRepo,
		analysisRepo,
		commentFormatter,
		docsPublisher,
	)
	googleOAuthService := service.NewGoogleOAuthService(
		googleOAuthRepo,
		googleOAuthProvider,
	)

	handler := api.NewRouter(api.Dependencies{
		AnalysisService: &analysisFacade{
			analysisService: analysisService,
			commentService:  commentService,
		},
		GoogleOAuthService: googleOAuthService,
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
