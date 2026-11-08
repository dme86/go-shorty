package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/example/shorty/internal/config"
	"github.com/example/shorty/internal/store"
)

func TestPublicResolveAndPreviewAreRateLimited(t *testing.T) {
	ms:=&mockStore{
		getLinkFn:func(context.Context,string)(store.Link,error){return store.Link{TenantID:"tenant",Code:"abc1234",LongURL:"https://example.com/",IsActive:true},nil},
	}
	h:=&Handlers{cfg:config.Config{
		PublicRateLimitRPS:0.0001,PublicRateLimitBurst:1,
	},store:ms}
	router:=chi.NewRouter()
	h.Mount(router)
	first:=httptest.NewRequest(http.MethodGet,"/abc1234",nil)
	first.RemoteAddr="203.0.113.10:10001"
	rec:=httptest.NewRecorder()
	router.ServeHTTP(rec,first)
	if rec.Code!=http.StatusFound {t.Fatalf("first redirect=%d",rec.Code)}
	second:=httptest.NewRequest(http.MethodGet,"/preview/abc1234",nil)
	second.RemoteAddr=first.RemoteAddr
	second.Header.Set("X-Forwarded-For","203.0.113.11")
	rec=httptest.NewRecorder()
	router.ServeHTTP(rec,second)
	if rec.Code!=http.StatusTooManyRequests {t.Fatalf("second public call=%d, want 429",rec.Code)}
	if ms.tryIncrementCalls!=1 {t.Fatalf("clicks=%d, want 1",ms.tryIncrementCalls)}
}

func TestRateLimiterMaxUniqueVisitors(t *testing.T) {
	l:=newIPRateLimiter(0.1,1,false)
	// Keep one reserved slot below the cap to avoid constructing 100k requests.
	l.visitors=make(map[string]*visitor,100000)
	for i:=0;i<100000;i++ {l.visitors[strconv.Itoa(i)]=&visitor{}}
	req:=httptest.NewRequest(http.MethodGet,"/",nil)
	req.RemoteAddr="198.51.100.99:1234"
	if l.allow(req) {t.Fatal("full visitor map accepted another address")}
}
