package httpx

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const (
	sessionCookieName = "shorty_session"
	stateCookieName   = "shorty_oauth_state"
)

type oauthUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
}

type ctxKey string

const ctxKeyAuthSubject ctxKey = "auth_subject"
const ctxKeyTenantID ctxKey = "tenant_id"

func (h *Handlers) oauthConfigured() bool {
	return h.cfg.OAuthClientID != "" &&
		h.cfg.OAuthClientSecret != "" &&
		h.cfg.OAuthAuthURL != "" &&
		h.cfg.OAuthTokenURL != "" &&
		h.cfg.OAuthRedirectURL != "" &&
		h.cfg.SessionSecret != ""
}

func (h *Handlers) oauthConfig() *oauth2.Config {
	scopes := []string{"openid", "profile", "email"}
	if h.cfg.OAuthScopes != "" {
		scopes = nil
		for _, s := range strings.Split(h.cfg.OAuthScopes, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				scopes = append(scopes, s)
			}
		}
		if len(scopes) == 0 {
			scopes = []string{"openid", "profile", "email"}
		}
	}
	return &oauth2.Config{
		ClientID:     h.cfg.OAuthClientID,
		ClientSecret: h.cfg.OAuthClientSecret,
		RedirectURL:  h.cfg.OAuthRedirectURL,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  h.cfg.OAuthAuthURL,
			TokenURL: h.cfg.OAuthTokenURL,
		},
	}
}

func (h *Handlers) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.oauthConfigured() {
			http.Error(w, "oauth is not configured", http.StatusServiceUnavailable)
			return
		}
		if subject, ok := h.validateSession(r); ok {
			tenantID := tenantFromSubject(subject)
			ctx := context.WithValue(r.Context(), ctxKeyAuthSubject, subject)
			ctx = context.WithValue(ctx, ctxKeyTenantID, tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func authSubjectFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	v := r.Context().Value(ctxKeyAuthSubject)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func tenantIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	v := r.Context().Value(ctxKeyTenantID)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func tenantFromSubject(subject string) string {
	s := strings.ToLower(strings.TrimSpace(subject))
	if s == "" {
		return "default"
	}
	if at := strings.LastIndex(s, "@"); at >= 0 && at < len(s)-1 {
		return sanitizeTenantID(s[at+1:])
	}
	return sanitizeTenantID(s)
}

func sanitizeTenantID(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" {
		return "default"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range in {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "default"
	}
	return out
}

func (h *Handlers) AuthLogin(w http.ResponseWriter, r *http.Request) {
	if !h.oauthConfigured() {
		http.Error(w, "oauth is not configured", http.StatusServiceUnavailable)
		return
	}
	state, err := randToken(24)
	if err != nil {
		http.Error(w, "cannot start oauth", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})
	http.Redirect(w, r, h.oauthConfig().AuthCodeURL(state), http.StatusFound)
}

func (h *Handlers) AuthCallback(w http.ResponseWriter, r *http.Request) {
	if !h.oauthConfigured() {
		http.Error(w, "oauth is not configured", http.StatusServiceUnavailable)
		return
	}
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: h.cookieSecure(), SameSite: http.SameSiteLaxMode})

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "missing oauth code", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tok, err := h.oauthConfig().Exchange(ctx, code)
	if err != nil {
		http.Error(w, "oauth exchange failed", http.StatusUnauthorized)
		return
	}

	user, err := h.fetchUserInfo(ctx, tok)
	if err != nil {
		http.Error(w, "cannot read user info", http.StatusUnauthorized)
		return
	}
	subject, err := h.verifiedSubject(user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	cookieVal, err := h.signSession(subject, time.Now().UTC().Add(12*time.Hour))
	if err != nil {
		http.Error(w, "cannot create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    cookieVal,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   12 * 60 * 60,
	})

	http.Redirect(w, r, "/", http.StatusFound)
}

func (h *Handlers) AuthLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: h.cookieSecure(), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/auth/login", http.StatusFound)
}

func (h *Handlers) fetchUserInfo(ctx context.Context, tok *oauth2.Token) (oauthUserInfo, error) {
	if h.cfg.OAuthUserInfoURL == "" {
		return oauthUserInfo{}, errors.New("OAUTH_USERINFO_URL not set")
	}
	client := h.oauthConfig().Client(ctx, tok)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.OAuthUserInfoURL, nil)
	if err != nil {
		return oauthUserInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return oauthUserInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return oauthUserInfo{}, fmt.Errorf("userinfo status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var u oauthUserInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&u); err != nil {
		return oauthUserInfo{}, err
	}
	return u, nil
}

// Never derive a domain-scoped tenant from an unverified email claim.
// A provider without emails may use its stable OpenID subject only when
// no email-domain allowlist is configured.
func (h *Handlers) verifiedSubject(user oauthUserInfo) (string, error) {
	sub := strings.TrimSpace(user.Sub)
	if sub == "" {
		return "", errors.New("oauth provider did not supply a subject")
	}
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if email == "" {
		if strings.TrimSpace(h.cfg.OAuthAllowedDomains) != "" {
			return "", errors.New("verified email required for domain allowlist")
		}
		return "sub:" + sub, nil
	}
	if user.EmailVerified == nil || !*user.EmailVerified {
		return "", errors.New("oauth email is not verified")
	}
	if !h.emailAllowed(email) {
		return "", errors.New("email domain not allowed")
	}
	return email, nil
}

func (h *Handlers) emailAllowed(email string) bool {
	domains := strings.TrimSpace(h.cfg.OAuthAllowedDomains)
	if domains == "" {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	domain := strings.ToLower(strings.TrimSpace(email[at+1:]))
	for _, d := range strings.Split(domains, ",") {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

func (h *Handlers) cookieSecure() bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(h.cfg.BaseURL)), "https://")
}

func (h *Handlers) signSession(subject string, exp time.Time) (string, error) {
	expUnix := exp.Unix()
	payload := "v2|" + subject + "|" + strconv.FormatInt(expUnix, 10)
	mac := hmac.New(sha256.New, []byte(h.cfg.SessionSecret))
	_, _ = mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	raw := payload + "|" + sig
	return base64.RawURLEncoding.EncodeToString([]byte(raw)), nil
}

func (h *Handlers) validateSession(r *http.Request) (string, bool) {
	ck, err := r.Cookie(sessionCookieName)
	if err != nil || ck.Value == "" {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(ck.Value)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "|")
	// v1 cookies were issued without email verification, and must not be
	// accepted after enabling the verified-identity policy.
	if len(parts) != 4 || parts[0] != "v2" || parts[1] == "" {
		return "", false
	}
	subject, expRaw, gotSig := parts[1], parts[2], parts[3]
	payload := "v2|" + subject + "|" + expRaw
	mac := hmac.New(sha256.New, []byte(h.cfg.SessionSecret))
	_, _ = mac.Write([]byte(payload))
	expectSig := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(gotSig), []byte(expectSig)) {
		return "", false
	}
	expUnix, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil || time.Now().UTC().After(time.Unix(expUnix, 0).UTC()) {
		return "", false
	}
	return subject, true
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
