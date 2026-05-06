package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL         string
	BaseURL             string
	Port                string
	Env                 string
	SessionSecret       string
	OAuthClientID       string
	OAuthClientSecret   string
	OAuthAuthURL        string
	OAuthTokenURL       string
	OAuthUserInfoURL    string
	OAuthRedirectURL    string
	OAuthScopes         string
	OAuthAllowedDomains string
	RateLimitRPS        float64
	RateLimitBurst      int
	TrustProxyHeaders   bool
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func Load() Config {
	return Config{
		DatabaseURL:         getenv("DATABASE_URL", "postgres://shorty:shorty@localhost:5432/shorty?sslmode=disable"),
		BaseURL:             getenv("BASE_URL", "http://localhost:8080"),
		Port:                getenv("PORT", "8080"),
		Env:                 getenv("ENV", "dev"),
		SessionSecret:       getenv("SESSION_SECRET", ""),
		OAuthClientID:       getenv("OAUTH_CLIENT_ID", ""),
		OAuthClientSecret:   getenv("OAUTH_CLIENT_SECRET", ""),
		OAuthAuthURL:        getenv("OAUTH_AUTH_URL", ""),
		OAuthTokenURL:       getenv("OAUTH_TOKEN_URL", ""),
		OAuthUserInfoURL:    getenv("OAUTH_USERINFO_URL", ""),
		OAuthRedirectURL:    getenv("OAUTH_REDIRECT_URL", ""),
		OAuthScopes:         getenv("OAUTH_SCOPES", "openid,profile,email"),
		OAuthAllowedDomains: getenv("OAUTH_ALLOWED_DOMAINS", ""),
		RateLimitRPS:        getenvFloat("RATE_LIMIT_RPS", 5),
		RateLimitBurst:      getenvInt("RATE_LIMIT_BURST", 20),
		TrustProxyHeaders:   getenvBool("TRUST_PROXY_HEADERS", false),
	}
}

func getenvInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func getenvFloat(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func getenvBool(k string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
