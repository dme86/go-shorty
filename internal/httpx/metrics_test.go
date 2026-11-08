package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/shorty/internal/config"
)

func TestMetricsRequireBearerToken(t *testing.T) {
	served := 0
	next := http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
		served++
		w.WriteHeader(http.StatusOK)
		_,_ = w.Write([]byte("secret metrics"))
	})
	h := &Handlers{cfg:config.Config{MetricsToken:"large-random-token"}}
	cases := []struct{ token string; want int }{
		{"",http.StatusUnauthorized},
		{"Bearer wrong",http.StatusUnauthorized},
		{"Basic large-random-token",http.StatusUnauthorized},
		{"Bearer large-random-token",http.StatusOK},
	}
	for _,tc := range cases {
		req := httptest.NewRequest(http.MethodGet,"/metrics",nil)
		if tc.token!="" { req.Header.Set("Authorization",tc.token) }
		rec := httptest.NewRecorder()
		h.ProtectMetrics(next).ServeHTTP(rec,req)
		if rec.Code!=tc.want {t.Fatalf("token %q: status=%d, want=%d",tc.token,rec.Code,tc.want)}
		if tc.want!=http.StatusOK && strings.Contains(rec.Body.String(),"secret metrics") {t.Fatal("unauthorized metrics response leaked data")}
	}
	if served!=1 {t.Fatalf("metrics handler invoked %d times, want once",served)}
	h.cfg.MetricsToken=""
	rec:=httptest.NewRecorder()
	h.ProtectMetrics(next).ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/metrics",nil))
	if rec.Code!=http.StatusNotFound {t.Fatalf("unset token: status=%d, want 404",rec.Code)}
}
