package config

import (
	"os"
	"testing"
)

func TestEnvOr(t *testing.T) {
	// Unset key returns fallback
	if err := os.Unsetenv("TEST_ENVOR_KEY"); err != nil {
		t.Fatal(err)
	}
	if got := envOr("TEST_ENVOR_KEY", "default"); got != "default" {
		t.Errorf("envOr unset key = %q, want %q", got, "default")
	}

	// Set key returns value
	t.Setenv("TEST_ENVOR_KEY", "custom")
	if got := envOr("TEST_ENVOR_KEY", "default"); got != "custom" {
		t.Errorf("envOr set key = %q, want %q", got, "custom")
	}

	// Empty string returns fallback
	t.Setenv("TEST_ENVOR_KEY", "")
	if got := envOr("TEST_ENVOR_KEY", "fallback"); got != "fallback" {
		t.Errorf("envOr empty key = %q, want %q", got, "fallback")
	}
}

func TestLoadDefaults(t *testing.T) {
	// Clear all relevant env vars
	for _, k := range []string{"PORT", "DATABASE_URL", "TELEGRAM_BOT_TOKEN", "FRONTEND_ORIGIN", "REDIS_URL", "REDIS_PASSWORD", "INFISICAL_CLIENT_ID", "INFISICAL_CLIENT_SECRET"} {
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}

	cfg := Load()

	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.FrontendOrigin != "*" {
		t.Errorf("FrontendOrigin = %q, want %q", cfg.FrontendOrigin, "*")
	}
	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
	if cfg.TelegramToken != "" {
		t.Errorf("TelegramToken = %q, want empty", cfg.TelegramToken)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("FRONTEND_ORIGIN", "http://localhost:3000")

	cfg := Load()

	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
	if cfg.DatabaseURL != "postgres://test" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://test")
	}
	if cfg.TelegramToken != "test-token" {
		t.Errorf("TelegramToken = %q, want %q", cfg.TelegramToken, "test-token")
	}
	if cfg.FrontendOrigin != "http://localhost:3000" {
		t.Errorf("FrontendOrigin = %q, want %q", cfg.FrontendOrigin, "http://localhost:3000")
	}
}
