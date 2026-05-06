package config

import "os"

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
	}
}
