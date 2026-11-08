package httpx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/example/shorty/internal/config"
)

func TestTenantIDsSeparateAmbiguousDomains(t *testing.T) {
	cases := []struct {
		subject string
		want string
	}{
		{"email:alice@a.b.com", "domain:a.b.com"},
		{"email:bob@a-b.com", "domain:a-b.com"},
		{"email:alice@EXAMPLE.com", "domain:example.com"},
		{"email:bob@example.com", "domain:example.com"},
		{"email:person@bücher.example", "domain:xn--bcher-kva.example"},
		{"sub:SubJect@Example.Com", "subject:U3ViSmVjdEBFeGFtcGxlLkNvbQ"},
		{"sub:subject@example.com", "subject:c3ViamVjdEBleGFtcGxlLmNvbQ"},
		{"", ""},
		{"alice@example.com", ""}, // Never accept the old untyped format.
		{"email:a@a..com", ""},
		{"email:a@a-b..com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.subject, func(t *testing.T) {
			if got := tenantFromSubject(tc.subject); got != tc.want {
				t.Fatalf("tenantFromSubject(%q)=%q, want %q",tc.subject,got,tc.want)
			}
		})
	}
	if tenantFromSubject("email:alice@a.b.com") == tenantFromSubject("email:bob@a-b.com") {
		t.Fatal("different DNS domains got the same tenant")
	}
}

func TestV3SessionRejectsLegacyV2Cookie(t *testing.T) {
	const key = "a-random-signing-secret"
	h := &Handlers{cfg: config.Config{SessionSecret:key}}
	exp := time.Now().Add(time.Hour)
	current,err := h.signSession("email:alice@a.b.com",exp)
	if err!=nil { t.Fatal(err) }
	req := httptest.NewRequest(http.MethodGet,"/",nil)
	req.AddCookie(&http.Cookie{Name:sessionCookieName,Value:current})
	if sub,ok:=h.validateSession(req);!ok||sub!="email:alice@a.b.com" {t.Fatalf("v3 session invalid: %q / %v",sub,ok)}
	legacy := "v2|alice@a.b.com|"+strconv.FormatInt(exp.Unix(),10)
	mac:=hmac.New(sha256.New,[]byte(key))
	_,_=mac.Write([]byte(legacy))
	old:=base64.RawURLEncoding.EncodeToString([]byte(legacy+"|"+hex.EncodeToString(mac.Sum(nil))))
	req=httptest.NewRequest(http.MethodGet,"/",nil)
	req.AddCookie(&http.Cookie{Name:sessionCookieName,Value:old})
	if _,ok:=h.validateSession(req);ok {t.Fatal("v2 cookie was accepted")}
}

func TestVerifiedSubjectUsesTypedIdentity(t *testing.T) {
	h := &Handlers{cfg:config.Config{}}
	email,err:=h.verifiedSubject(oauthUserInfo{Sub:"user-1",Email:"Alice@a.b.com",EmailVerified:verified(true)})
	if err!=nil||email!="email:alice@a.b.com" {t.Fatalf("email=%q err=%v",email,err)}
	sub,err:=h.verifiedSubject(oauthUserInfo{Sub:"Foo@Example",Email:""})
	if err!=nil||sub!="sub:Foo@Example" {t.Fatalf("subject=%q err=%v",sub,err)}
	if tenantFromSubject(email)==tenantFromSubject(sub) {t.Fatal("email and sub identities collided")}
}
