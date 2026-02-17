package config

import "os"

type Config struct {
	Port            string
	DatabaseURL     string
	TelegramToken   string
	FrontendOrigin  string
}

func Load() Config {
	return Config{
		Port:           envOr("PORT", "8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		TelegramToken:  os.Getenv("TELEGRAM_BOT_TOKEN"),
		FrontendOrigin: envOr("FRONTEND_ORIGIN", "*"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
