package httpx

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// Public redirects may point to internal URLs, but the metadata fetcher
// independently enforces a stricter SSRF-safe network policy.
func validateLongURL(raw string) error {
	if raw == "" || len(raw) > 2048 {
		return errors.New("URL must contain 1 to 2048 bytes")
	}
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return errors.New("URL contains control characters")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("malformed URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" ||
		u.Host == "" || u.Hostname() == "" || u.User != nil {
		return errors.New("URL requires an http/https scheme, hostname, and no credentials")
	}
	if strings.ContainsAny(u.Hostname(), "%\\") {
		return errors.New("invalid hostname")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("invalid port")
		}
	}
	return nil
}
