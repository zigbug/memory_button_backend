package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"memorybutton/backend/internal/ai/providerfactory"
	"memorybutton/backend/internal/auth"
	"memorybutton/backend/internal/config"
	httprouter "memorybutton/backend/internal/http"
	"memorybutton/backend/internal/http/handlers"
	"memorybutton/backend/internal/pipeline"
	"memorybutton/backend/internal/storage/media"
	"memorybutton/backend/internal/storage/postgres"
	"memorybutton/backend/internal/storage/s3"
	"memorybutton/backend/internal/worker"
	"memorybutton/backend/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("ошибка конфигурации", "err", err)
		os.Exit(1)
	}
	logger.Info("конфигурация загружена", "http", cfg.HTTPAddr, "ai_provider", cfg.AI.Provider)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := postgres.NewStore(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		logger.Error("не удалось подключиться к PostgreSQL", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := postgres.RunMigrations(ctx, store.Pool(), migrations.FS); err != nil {
		logger.Error("миграции не применены", "err", err)
		os.Exit(1)
	}
	logger.Info("миграции применены")

	// Single-user bootstrap.
	hash, err := ensureUserHash(cfg.Auth)
	if err != nil {
		logger.Error("не удалось подготовить учётную запись", "err", err)
		os.Exit(1)
	}
	if err := store.EnsureUser(ctx, cfg.Auth.Username, hash); err != nil {
		logger.Error("не удалось создать пользователя", "err", err)
		os.Exit(1)
	}

	var storage s3.Storage
	if cfg.S3.Driver == "fs" {
		storage, err = s3.NewFS(cfg.S3.Path, cfg.S3.PublicURL)
		if err != nil {
			logger.Error("не удалось инициализировать файловое хранилище", "err", err)
			os.Exit(1)
		}
		logger.Info("используется файловое хранилище", "path", cfg.S3.Path)
	} else {
		storage, err = s3.NewMinio(cfg.S3)
		if err != nil {
			logger.Error("не удалось подключиться к S3", "err", err)
			os.Exit(1)
		}
	}
	mediaSvc := media.NewService(store, storage)

	provider := providerfactory.Build(cfg.AI)
	interviewerModel, extractorModel := cfg.AI.ResolveModels()
	dialogue := pipeline.NewDialogue(store, provider, mediaSvc, logger)
	post := pipeline.NewPostprocessor(store, provider, mediaSvc, logger, interviewerModel, extractorModel)
	qa := pipeline.NewQAService(store, provider, logger)

	h := handlers.New(store, provider, mediaSvc, storage, dialogue, post, qa, cfg, logger)
	router := httprouter.NewRouter(h, cfg, store, logger)

	// Воркер (если не синхронный режим).
	if cfg.Worker.Enabled && !cfg.Worker.SyncProcessing {
		w := worker.New(store, post, logger, cfg.Worker.PollInterval)
		go func() {
			_ = w.Run(ctx)
		}()
	}

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: router}
	go func() {
		logger.Info("HTTP-сервер запущен", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("ошибка сервера", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("завершение работы")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	logger.Info("сервер остановлен")
}

func ensureUserHash(a config.AuthConfig) (string, error) {
	if a.PasswordHash != "" {
		return a.PasswordHash, nil
	}
	return auth.HashPassword(a.Password, a.BcryptCost)
}
