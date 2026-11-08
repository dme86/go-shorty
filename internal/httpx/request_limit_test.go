package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/shorty/internal/config"
)

func TestCreateLinkRejectsOversizedJSON(t *testing.T) {
	h:=&Handlers{cfg:config.Config{}}
	req:=httptest.NewRequest(http.MethodPost,"/api/links",strings.NewReader(`{"url":"https://example.com/?q=`+strings.Repeat("a",16<<10)+`"}`))
	req=req.WithContext(context.WithValue(req.Context(),ctxKeyTenantID,"test"))
	rec:=httptest.NewRecorder()
	h.API_CreateLink(rec,req)
	if rec.Code!=http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized JSON status=%d, want 413: %s",rec.Code,rec.Body.String())
	}
}
