package config

import "os"

type Config struct {
	DatabaseURL string
	BaseURL     string
	Port        string
	Env         string
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func Load() Config {
	return Config{
		DatabaseURL: getenv("DATABASE_URL", "postgres://shorty:shorty@localhost:5432/shorty?sslmode=disable"),
		BaseURL:     getenv("BASE_URL", "http://localhost:8080"),
		Port:        getenv("PORT", "8080"),
		Env:         getenv("ENV", "dev"),
	}
}
