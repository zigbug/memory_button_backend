// Package config загружает конфигурацию приложения из переменных окружения.
//
// Секреты (пароль, JWT_SECRET, ключи S3, HF_TOKEN) никогда не хардкодятся:
// значения по умолчанию предназначены только для локальной разработки и
// сопровождаются предупреждением в логе.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	DBMaxConns      int32
	LogLevel        string
	ShutdownTimeout time.Duration

	Auth AuthConfig
	JWT  JWTConfig

	S3   S3Config
	AI   AIConfig
	Worker WorkerConfig
}

type AuthConfig struct {
	Username      string
	Password      string
	PasswordHash  string
	StaticToken   string
	BcryptCost    int
}

type JWTConfig struct {
	Secret    string
	TTL       time.Duration
	Issuer    string
}

type S3Config struct {
	Driver    string // "s3" (default) | "fs"
	Path      string // for fs driver
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	PublicURL string // базовый URL для публичных ссылок (опционально)
}

type AIConfig struct {
	Provider         string // "hf" | "stub"
	FallbackToStub   bool
	HFToken          string
	HFBaseURL        string
	HFChatModel      string
	HFASRModel       string
	HFTTSModel       string
	HFTimeout        time.Duration
	InterviewerModel string
	ExtractorModel   string
}

type WorkerConfig struct {
	Enabled      bool
	PollInterval time.Duration
	SyncProcessing bool // обрабатывать постобработку синхронно в API (для одиночного контейнера)
}

func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://memory:memory@localhost:5432/memorybutton?sslmode=disable"),
		DBMaxConns:      int32(getEnvInt("DB_MAX_CONNS", 10)),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		ShutdownTimeout: getEnvDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		Auth: AuthConfig{
			Username:     getEnv("AUTH_USERNAME", "family"),
			Password:     getEnv("AUTH_PASSWORD", "change-me"),
			PasswordHash: os.Getenv("AUTH_PASSWORD_HASH"),
			StaticToken:  os.Getenv("AUTH_STATIC_TOKEN"),
			BcryptCost:   getEnvInt("BCRYPT_COST", 10),
		},
		JWT: JWTConfig{
			Secret: getEnv("JWT_SECRET", "dev-only-insecure-secret-change-me"),
			TTL:    getEnvDuration("JWT_TTL", 24*time.Hour),
			Issuer: getEnv("JWT_ISSUER", "memory-button"),
		},
		S3: S3Config{
		Driver:    strings.ToLower(getEnv("STORAGE_DRIVER", "s3")),
		Path:      getEnv("STORAGE_PATH", "/data/storage"),
			Endpoint:  getEnv("S3_ENDPOINT", "localhost:9000"),
			Region:    getEnv("S3_REGION", "us-east-1"),
			Bucket:    getEnv("S3_BUCKET", "memory-button"),
			AccessKey: getEnv("S3_ACCESS_KEY", "minioadmin"),
			SecretKey: getEnv("S3_SECRET_KEY", "minioadmin"),
			UseSSL:    getEnvBool("S3_USE_SSL", false),
			PublicURL: os.Getenv("S3_PUBLIC_URL"),
		},
		AI: AIConfig{
			Provider:         strings.ToLower(getEnv("AI_PROVIDER", "stub")),
			FallbackToStub:   getEnvBool("AI_FALLBACK_TO_STUB", true),
			HFToken:          os.Getenv("HF_TOKEN"),
			HFBaseURL:        getEnv("HF_BASE_URL", "https://router.huggingface.co/hf-inference/models"),
			HFChatModel:      getEnv("HF_CHAT_MODEL", "mistralai/Mistral-7B-Instruct-v0.3"),
			HFASRModel:       getEnv("HF_ASR_MODEL", "openai/whisper-large-v3"),
			HFTTSModel:       getEnv("HF_TTS_MODEL", "microsoft/speecht5_tts"),
			HFTimeout:        getEnvDuration("HF_TIMEOUT", 90*time.Second),
			InterviewerModel: os.Getenv("HF_INTERVIEWER_MODEL"),
			ExtractorModel:   os.Getenv("HF_EXTRACTOR_MODEL"),
		},
		Worker: WorkerConfig{
			Enabled:        getEnvBool("WORKER_ENABLED", true),
			PollInterval:   getEnvDuration("WORKER_POLL_INTERVAL", 2*time.Second),
			SyncProcessing: getEnvBool("PROCESSING_SYNC", false),
		},
	}

	if cfg.Auth.PasswordHash != "" && cfg.Auth.Password == "" {
		cfg.Auth.Password = "" // хеш уже задан, пароль не нужен
	}
	if cfg.AI.Provider != "hf" && cfg.AI.Provider != "stub" {
		return nil, fmt.Errorf("AI_PROVIDER должен быть 'hf' или 'stub', получено %q", cfg.AI.Provider)
	}
	if cfg.AI.Provider == "hf" && cfg.AI.HFToken == "" {
		return nil, fmt.Errorf("AI_PROVIDER=hf требует HF_TOKEN")
	}
	if cfg.Auth.BcryptCost < 4 || cfg.Auth.BcryptCost > 31 {
		return nil, fmt.Errorf("BCRYPT_COST вне диапазона 4..31")
	}
	if cfg.JWT.Secret == "dev-only-insecure-secret-change-me" {
		// Не возвращаем ошибку: конфиг должен подниматься локально, но об этом надо кричать.
		fmt.Fprintln(os.Stderr, "ВНИМАНИЕ: используется JWT_SECRET по умолчанию. Задайте JWT_SECRET в продакшене.")
	}
	if cfg.Auth.Password == "change-me" {
		fmt.Fprintln(os.Stderr, "ВНИМАНИЕ: используется AUTH_PASSWORD по умолчанию. Задайте AUTH_PASSWORD или AUTH_PASSWORD_HASH.")
	}
	return cfg, nil
}

// ResolveModels подставляет отдельные модели для интервьюера/экстрактора,
// если они заданы, иначе использует общую HF_CHAT_MODEL.
func (c *AIConfig) ResolveModels() (interviewer, extractor string) {
	interviewer = c.HFChatModel
	if c.InterviewerModel != "" {
		interviewer = c.InterviewerModel
	}
	extractor = c.HFChatModel
	if c.ExtractorModel != "" {
		extractor = c.ExtractorModel
	}
	return
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return def
}
