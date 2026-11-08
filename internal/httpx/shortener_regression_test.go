package httpx

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/example/shorty/internal/config"
	"github.com/example/shorty/internal/store"
)

func createRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(body))
	return r.WithContext(context.WithValue(r.Context(), ctxKeyTenantID, "domain:example.com"))
}

func TestValidateLongURL(t *testing.T) {
	invalid := []string{
		"", "http", "https://", "http:example.com", "javascript:alert(1)",
		"mailto:alice@example.com", "ftp://example.com", "https://user:secret@example.com/",
		"http://:80/path", "http://example.com:0/", "http://example.com:65536/",
		"http://example.com:abc/", "https://[::1", "https://example.com/\r\nHost: evil",
		"https://example.com/" + strings.Repeat("x", 2048),
	}
	for _, raw := range invalid {
		if err := validateLongURL(raw); err == nil {
			t.Errorf("invalid URL allowed: %q", raw)
		}
	}
	valid := []string{
		"https://example.com/path?q=one#frag",
		"http://localhost:8080/", "https://[2606:4700:4700::1111]/",
		"https://example.com:443/",
	}
	for _, raw := range valid {
		if err := validateLongURL(raw); err != nil {
			t.Errorf("valid URL %q rejected: %v", raw, err)
		}
	}
}

func TestCreateRejectsInvalidURLBeforeDatabaseWrites(t *testing.T) {
	ms := &mockStore{}
	h := &Handlers{store: ms}
	for _, url := range []string{"httpjunk", "javascript:alert(1)", "https://user:pw@example.com"} {
		body, err := json.Marshal(map[string]string{"url": url})
		if err != nil { t.Fatal(err) }
		rec := httptest.NewRecorder()
		h.API_CreateLink(rec, createRequest(string(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d want=400", url, rec.Code)
		}
	}
	if ms.createCalls != 0 { t.Fatal("invalid URL was inserted") }
}

func TestCreateRetriesGlobalCodeCollisions(t *testing.T) {
	codes := []string{"ABC1234", "ABC1234", "XYZ9876"}
	next := 0
	ms := &mockStore{createFn: func(ctx context.Context, tenantID string, l store.Link) error {
		if l.Code == "ABC1234" {
			return store.ErrCodeTaken
		}
		return nil
	}}
	h := &Handlers{
		cfg: config.Config{BaseURL: "https://short.example.com"},
		store: ms,
		newCode: func() (string, error) {
			if next >= len(codes) { return "", errors.New("extra attempt") }
			code := codes[next]
			next++
			return code, nil
		},
	}
	rec := httptest.NewRecorder()
	h.API_CreateLink(rec, createRequest(`{"url":"https://example.com/"}`))
	if rec.Code != http.StatusOK || ms.createCalls != 3 {
		t.Fatalf("create status=%d attempts=%d: %s", rec.Code, ms.createCalls, rec.Body.String())
	}
	var response struct {
		Code string `json:"code"`
		QRURL string `json:"qr_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil { t.Fatal(err) }
	if response.Code != "XYZ9876" || response.QRURL != "https://short.example.com/qr/XYZ9876" {
		t.Fatalf("unexpected response %+v",response)
	}
}

func TestCreateFailsClosedOnExhaustedCodes(t *testing.T) {
	ms := &mockStore{createFn: func(context.Context, string, store.Link) error { return store.ErrCodeTaken }}
	h := &Handlers{store: ms, newCode: func() (string,error) { return "ABC1234", nil }}
	rec := httptest.NewRecorder()
	h.API_CreateLink(rec,createRequest(`{"url":"https://example.com/"}`))
	if rec.Code != http.StatusServiceUnavailable || ms.createCalls!=10 {
		t.Fatalf("exhausted codes status=%d, attempts=%d",rec.Code,ms.createCalls)
	}
}

func TestDedupeReceivesExactLimitParameters(t *testing.T) {
	expiry := time.Date(2027,1,2,3,4,5,123456789,time.FixedZone("shift",3600))
	ms := &mockStore{findFn: func(ctx context.Context, tenantID, url string, expires *time.Time, max *int64)(store.Link,error) {
		if tenantID!="domain:example.com" || url!="https://example.com/" || expires==nil || max==nil {
			t.Fatalf("missing dedupe parameters tenant=%q url=%q expires=%v max=%v",tenantID,url,expires,max)
		}
		want:=expiry.UTC().Truncate(time.Microsecond)
		if !expires.Equal(want) || expires.Location()!=time.UTC || *max!=10 {
			t.Fatalf("dedupe limit mismatch: expires=%v want=%v max=%d",expires,want,*max)
		}
		return store.Link{Code:"EXIST12"},nil
	}}
	h:=&Handlers{cfg:config.Config{BaseURL:"https://short.example.com"},store:ms}
	body:=fmt.Sprintf(`{"url":"https://example.com/","expires_at":%q,"max_clicks":10}`,expiry.Format(time.RFC3339Nano))
	rec:=httptest.NewRecorder()
	h.API_CreateLink(rec,createRequest(body))
	if rec.Code!=http.StatusOK||ms.createCalls!=0 {t.Fatalf("dedupe response=%d creates=%d",rec.Code,ms.createCalls)}
	if !strings.Contains(rec.Body.String(),"EXIST12") {t.Fatalf("unexpected dedupe response: %s",rec.Body.String())}
}

func TestCreateDoesNotDedupeOnDatabaseFailure(t *testing.T) {
	ms:=&mockStore{findFn:func(context.Context,string,string,*time.Time,*int64)(store.Link,error){return store.Link{},sql.ErrConnDone}}
	h:=&Handlers{store:ms}
	rec:=httptest.NewRecorder()
	h.API_CreateLink(rec,createRequest(`{"url":"https://example.com/"}`))
	if rec.Code!=http.StatusInternalServerError||ms.createCalls!=0 {
		t.Fatalf("unexpected response=%d creates=%d",rec.Code,ms.createCalls)
	}
}

func TestAnonymousPublicQRAndExpiredOrExhaustedGuards(t *testing.T) {
	active:=store.Link{TenantID:"domain:example.com",Code:"abc1234",LongURL:"https://example.com/",IsActive:true}
	link:=active
	ms:=&mockStore{getLinkFn:func(context.Context,string)(store.Link,error){return link,nil}}
	h:=&Handlers{store:ms,cfg:config.Config{BaseURL:"https://short.example.com",PublicRateLimitRPS:30,PublicRateLimitBurst:100}}
	router:=chi.NewRouter()
	h.Mount(router)
	wantPNG:=[]byte{137,80,78,71,13,10,26,10}
	tests:=[]struct{name string;update func();want int}{
		{"active",func(){link=active},http.StatusOK},
		{"inactive",func(){link=active;link.IsActive=false},http.StatusNotFound},
		{"expired",func(){link=active;link.ExpiresAt=sql.NullTime{Valid:true,Time:time.Now().Add(-time.Minute)}},http.StatusNotFound},
		{"maxed",func(){link=active;link.ClickCount=3;link.MaxClicks=sql.NullInt64{Valid:true,Int64:3}},http.StatusNotFound},
	}
	for i,tc:=range tests{
		tc.update()
		req:=httptest.NewRequest(http.MethodGet,"/qr/abc1234",nil)
		req.RemoteAddr=fmt.Sprintf("203.0.113.%d:5000",i+1)
		rec:=httptest.NewRecorder()
		router.ServeHTTP(rec,req)
		if rec.Code!=tc.want {t.Fatalf("%s status=%d want=%d",tc.name,rec.Code,tc.want)}
		if tc.want==http.StatusOK {
			if rec.Header().Get("Content-Type")!="image/png" || len(rec.Body.Bytes())<len(wantPNG) {
				t.Fatalf("QR is not PNG")
			}
			for j,b:=range wantPNG {if rec.Body.Bytes()[j]!=b{t.Fatal("PNG header mismatch")}}
		}
	}
	// Only public QR access was used: no redirect/click should be counted.
	if ms.tryIncrementCalls!=0 {t.Fatalf("QR counted clicks: %d",ms.tryIncrementCalls)}
	// The older tenant-scoped endpoint must NOT become anonymously readable.
	rec:=httptest.NewRecorder()
	router.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/api/links/abc1234/qr.png",nil))
	if rec.Code==http.StatusOK {t.Fatal("tenant API QR exposed without OAuth")}
}
