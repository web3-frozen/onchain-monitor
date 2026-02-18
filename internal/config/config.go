package config

import (
	"context"
	"log/slog"
	"os"
	"time"

	infisical "github.com/infisical/go-sdk"
)

type Config struct {
	Port           string
	DatabaseURL    string
	TelegramToken  string
	FrontendOrigin string
	RedisURL       string
	RedisPassword  string
}

func Load() Config {
	cfg := Config{
		Port:           envOr("PORT", "8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		TelegramToken:  os.Getenv("TELEGRAM_BOT_TOKEN"),
		FrontendOrigin: envOr("FRONTEND_ORIGIN", "*"),
		RedisURL:       envOr("REDIS_URL", "redis://redis-master.redis.svc.cluster.local:6379/0"),
		RedisPassword:  os.Getenv("REDIS_PASSWORD"),
	}

	// If Infisical credentials are available, fetch secrets from Infisical
	clientID := os.Getenv("INFISICAL_CLIENT_ID")
	clientSecret := os.Getenv("INFISICAL_CLIENT_SECRET")
	if clientID != "" && clientSecret != "" {
		loadFromInfisical(&cfg, clientID, clientSecret)
	}

	return cfg
}

func loadFromInfisical(cfg *Config, clientID, clientSecret string) {
	siteURL := envOr("INFISICAL_SITE_URL",
		"http://infisical-infisical-standalone-infisical.infisical.svc.cluster.local:8080")
	projectID := os.Getenv("INFISICAL_PROJECT_ID")
	envSlug := envOr("INFISICAL_ENV", "prod")

	if projectID == "" {
		slog.Warn("INFISICAL_PROJECT_ID not set, skipping Infisical")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := infisical.NewInfisicalClient(ctx, infisical.Config{
		SiteUrl:          siteURL,
		AutoTokenRefresh: false,
	})

	_, err := client.Auth().UniversalAuthLogin(clientID, clientSecret)
	if err != nil {
		slog.Error("infisical auth failed", "error", err)
		return
	}

	secrets := map[string]*string{
		"TELEGRAM_BOT_TOKEN": &cfg.TelegramToken,
		"REDIS_PASSWORD":     &cfg.RedisPassword,
	}

	for key, target := range secrets {
		if *target != "" {
			continue // env var already set, skip
		}
		secret, err := client.Secrets().Retrieve(infisical.RetrieveSecretOptions{
			SecretKey:   key,
			Environment: envSlug,
			ProjectID:   projectID,
			SecretPath:  "/",
		})
		if err != nil {
			slog.Warn("failed to retrieve secret from infisical", "key", key, "error", err)
			continue
		}
		*target = secret.SecretValue
		slog.Info("loaded secret from infisical", "key", key)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
