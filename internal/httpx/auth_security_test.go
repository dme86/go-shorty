package httpx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/example/shorty/internal/config"
)

func verified(v bool) *bool { return &v }

func TestVerifiedOAuthIdentity(t *testing.T) {
	cases := []struct {
		name string
		allowedDomains string
		user oauthUserInfo
		wantSubject string
		wantErr bool
	}{
		{"verified email", "example.com", oauthUserInfo{Sub:"user-1",Email:" ALICE@Example.com ",EmailVerified:verified(true)}, "alice@example.com", false},
		{"unverified email", "", oauthUserInfo{Sub:"user-1",Email:"alice@example.com",EmailVerified:verified(false)}, "", true},
		{"missing verification", "", oauthUserInfo{Sub:"user-1",Email:"alice@example.com"}, "", true},
		{"wrong domain", "example.com", oauthUserInfo{Sub:"user-1",Email:"alice@evil.test",EmailVerified:verified(true)}, "", true},
		{"missing sub", "", oauthUserInfo{Email:"alice@example.com",EmailVerified:verified(true)}, "", true},
		{"sub fallback", "", oauthUserInfo{Sub:"stable-opaque"}, "sub:stable-opaque", false},
		{"sub with allowlist", "example.com", oauthUserInfo{Sub:"stable-opaque"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handlers{cfg:config.Config{OAuthAllowedDomains:tc.allowedDomains}}
			got, err := h.verifiedSubject(tc.user)
			if (err != nil) != tc.wantErr || got != tc.wantSubject {
				t.Fatalf("verifiedSubject()=(%q,%v), want (%q,err=%v)", got,err,tc.wantSubject,tc.wantErr)
			}
		})
	}
}

func TestOldOAuthSessionCookiesAreRejected(t *testing.T) {
	const secret = "correct-horse-battery-staple"
	h := &Handlers{cfg:config.Config{SessionSecret:secret}}
	exp := time.Now().Add(time.Hour)
	value,err := h.signSession("alice@example.com",exp)
	if err != nil { t.Fatal(err) }
	req := httptest.NewRequest(http.MethodGet,"/",nil)
	req.AddCookie(&http.Cookie{Name:sessionCookieName,Value:value})
	if subject,ok := h.validateSession(req); !ok || subject!="alice@example.com" {
		t.Fatalf("new session failed validation: %q, %v",subject,ok)
	}
	oldPayload := "alice@example.com|"+strconv.FormatInt(exp.Unix(),10)
	mac := hmac.New(sha256.New,[]byte(secret))
	_,_ = mac.Write([]byte(oldPayload))
	old := base64.RawURLEncoding.EncodeToString([]byte(oldPayload+"|"+hex.EncodeToString(mac.Sum(nil))))
	req = httptest.NewRequest(http.MethodGet,"/",nil)
	req.AddCookie(&http.Cookie{Name:sessionCookieName,Value:old})
	if _,ok := h.validateSession(req); ok { t.Fatal("legacy session bypassed verified-email policy") }
	if strings.Contains(value,"alice@example.com") { t.Fatal("session exposes unencoded subject") }
}
