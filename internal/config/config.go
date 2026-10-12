package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Address           string
	DatabaseURL       string
	IngestionTimeout  time.Duration
	StateReadTimeout  time.Duration
	UserAgent         string
	RequestGap        time.Duration
	RateLimitCooldown time.Duration
	MaxImageBytes     int64
}

func Load() (Config, error) {
	var cfg Config

	port, err := portEnv("APP_PORT", "8080")
	if err != nil {
		return cfg, err
	}

	cfg.Address = net.JoinHostPort(
		env("APP_HOST", "127.0.0.1"),
		port,
	)

	cfg.UserAgent = env("XKCD_USER_AGENT", "xkcd-archive-demo/0.1")

	settings := []struct {
		name     string
		fallback string
		target   *time.Duration
	}{
		{"INGESTION_TIMEOUT", "120s", &cfg.IngestionTimeout},
		{"STATE_READ_TIMEOUT", "10s", &cfg.StateReadTimeout},
		{"XKCD_REQUEST_GAP", "30s", &cfg.RequestGap},
		{"XKCD_RATE_LIMIT_COOLDOWN", "15m", &cfg.RateLimitCooldown},
	}

	for _, setting := range settings {
		value, err := durationEnv(setting.name, setting.fallback)
		if err != nil {
			return cfg, err
		}
		*setting.target = value
	}

	if cfg.IngestionTimeout <= 2*cfg.RequestGap {
		return cfg, fmt.Errorf(
			"INGESTION_TIMEOUT must exceed twice XKCD_REQUEST_GAP",
		)
	}

	cfg.MaxImageBytes, err = strconv.ParseInt(
		env("XKCD_MAX_IMAGE_BYTES", "20971520"),
		10,
		64,
	)
	if err != nil || cfg.MaxImageBytes < 1 ||
		cfg.MaxImageBytes > 100*1024*1024 {
		return cfg, fmt.Errorf(
			"XKCD_MAX_IMAGE_BYTES must be between 1 and 104857600",
		)
	}

	dbPort, err := portEnv("POSTGRES_PORT", "5432")
	if err != nil {
		return cfg, err
	}

	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		return cfg, fmt.Errorf("POSTGRES_PASSWORD is required")
	}

	connection := &url.URL{
		Scheme: "postgres",
		User: url.UserPassword(
			env("POSTGRES_USER", "xkcd"),
			password,
		),
		Host: net.JoinHostPort(
			env("POSTGRES_HOST", "127.0.0.1"),
			dbPort,
		),
		Path: "/" + env("POSTGRES_DB", "xkcd"),
	}

	query := connection.Query()
	query.Set("sslmode", env("POSTGRES_SSLMODE", "disable"))
	connection.RawQuery = query.Encode()

	cfg.DatabaseURL = connection.String()

	return cfg, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func portEnv(name, fallback string) (string, error) {
	value := env(name, fallback)

	number, err := strconv.Atoi(value)
	if err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf(
			"%s must be a valid port, got %q",
			name,
			value,
		)
	}

	return strconv.Itoa(number), nil
}

func durationEnv(name, fallback string) (time.Duration, error) {
	value := env(name, fallback)

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf(
			"%s must be a positive duration, got %q",
			name,
			value,
		)
	}

	return duration, nil
}
