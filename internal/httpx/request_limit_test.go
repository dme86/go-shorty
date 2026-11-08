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

func TestCreateLinkRejectsOversizedTrailingWhitespace(t *testing.T) {
	h:=&Handlers{cfg:config.Config{}}
	req:=httptest.NewRequest(http.MethodPost,"/api/links",strings.NewReader(`{"url":"https://example.com"}`+strings.Repeat(" ",16<<10)))
	req=req.WithContext(context.WithValue(req.Context(),ctxKeyTenantID,"test"))
	rec:=httptest.NewRecorder()
	h.API_CreateLink(rec,req)
	if rec.Code!=http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized trailing whitespace: status=%d, want 413",rec.Code)
	}
}

func TestCreateLinkRejectsMultipleJSONDocuments(t *testing.T) {
	h:=&Handlers{cfg:config.Config{}}
	req:=httptest.NewRequest(http.MethodPost,"/api/links",strings.NewReader(`{"url":"https://example.com"}{"url":"https://another.example"}`))
	req=req.WithContext(context.WithValue(req.Context(),ctxKeyTenantID,"test"))
	rec:=httptest.NewRecorder()
	h.API_CreateLink(rec,req)
	if rec.Code!=http.StatusBadRequest {
		t.Fatalf("multiple JSON documents: status=%d, want 400",rec.Code)
	}
}
