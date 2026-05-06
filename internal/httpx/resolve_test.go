package httpx

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/example/shorty/internal/config"
	"github.com/example/shorty/internal/store"
)

type mockStore struct {
	getLinkFn           func(ctx context.Context, code string) (store.Link, error)
	tryIncrementClickFn func(ctx context.Context, code, ua, referer, country string) (bool, error)
	tryIncrementCalls   int
}

func (m *mockStore) CreateLink(ctx context.Context, l store.Link) error { return nil }
func (m *mockStore) GetLink(ctx context.Context, code string) (store.Link, error) {
	if m.getLinkFn != nil {
		return m.getLinkFn(ctx, code)
	}
	return store.Link{}, store.ErrNotFound
}
func (m *mockStore) ListLinks(ctx context.Context, limit int) ([]store.Link, error) { return nil, nil }
func (m *mockStore) IncrementClick(ctx context.Context, code, ua, referer, country string) error {
	return nil
}
func (m *mockStore) TryIncrementClick(ctx context.Context, code, ua, referer, country string) (bool, error) {
	m.tryIncrementCalls++
	if m.tryIncrementClickFn != nil {
		return m.tryIncrementClickFn(ctx, code, ua, referer, country)
	}
	return true, nil
}
func (m *mockStore) FindActiveByLongURL(ctx context.Context, longURL string) (store.Link, error) {
	return store.Link{}, store.ErrNotFound
}
func (m *mockStore) ListLinksByTag(ctx context.Context, tag string, limit int) ([]store.Link, error) {
	return nil, nil
}
func (m *mockStore) GetStats(ctx context.Context, code string) (store.Stats, error) {
	return store.Stats{}, store.ErrNotFound
}
func (m *mockStore) UpdateMeta(ctx context.Context, code string, md store.Meta) error { return nil }
func (m *mockStore) Ping(ctx context.Context) error                                   { return nil }
func (m *mockStore) CountLinks(ctx context.Context) (int64, error)                    { return 0, nil }
func (m *mockStore) CountActiveLinks(ctx context.Context) (int64, error)              { return 0, nil }
func (m *mockStore) CountDistinctTags(ctx context.Context) (int64, error)             { return 0, nil }
func (m *mockStore) SumClicks(ctx context.Context) (int64, error)                     { return 0, nil }

func TestResolve_GuardsAndRedirect(t *testing.T) {
	now := time.Now().UTC()
	baseLink := store.Link{
		Code:       "abc1234",
		LongURL:    "https://example.com/path",
		IsActive:   true,
		ClickCount: 0,
	}

	tests := []struct {
		name            string
		link            store.Link
		getErr          error
		tryAllowed      bool
		tryErr          error
		wantStatus      int
		wantLocation    string
		wantTryIncCalls int
	}{
		{name: "not found", getErr: store.ErrNotFound, wantStatus: http.StatusNotFound, wantTryIncCalls: 0},
		{name: "inactive", link: func() store.Link { l := baseLink; l.IsActive = false; return l }(), wantStatus: http.StatusNotFound, wantTryIncCalls: 0},
		{name: "expired", link: func() store.Link {
			l := baseLink
			l.ExpiresAt = sql.NullTime{Valid: true, Time: now.Add(-time.Minute)}
			return l
		}(), wantStatus: http.StatusNotFound, wantTryIncCalls: 0},
		{name: "max clicks reached", link: func() store.Link {
			l := baseLink
			l.MaxClicks = sql.NullInt64{Valid: true, Int64: 3}
			l.ClickCount = 3
			return l
		}(), wantStatus: http.StatusNotFound, wantTryIncCalls: 0},
		{name: "increment denied", link: baseLink, tryAllowed: false, wantStatus: http.StatusNotFound, wantTryIncCalls: 1},
		{name: "increment db error", link: baseLink, tryErr: errors.New("db fail"), wantStatus: http.StatusInternalServerError, wantTryIncCalls: 1},
		{name: "redirect success", link: baseLink, tryAllowed: true, wantStatus: http.StatusFound, wantLocation: "https://example.com/path", wantTryIncCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := &mockStore{}
			if tt.getErr != nil {
				ms.getLinkFn = func(context.Context, string) (store.Link, error) { return store.Link{}, tt.getErr }
			} else {
				link := tt.link
				ms.getLinkFn = func(context.Context, string) (store.Link, error) { return link, nil }
			}
			ms.tryIncrementClickFn = func(context.Context, string, string, string, string) (bool, error) {
				if tt.tryErr != nil {
					return false, tt.tryErr
				}
				return tt.tryAllowed, nil
			}

			h := &Handlers{cfg: config.Config{BaseURL: "http://localhost:8080"}, store: ms}
			req := httptest.NewRequest(http.MethodGet, "/abc1234", nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("code", "abc1234")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			rr := httptest.NewRecorder()

			h.Resolve(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
			if tt.wantLocation != "" {
				if got := rr.Header().Get("Location"); got != tt.wantLocation {
					t.Fatalf("location = %q, want %q", got, tt.wantLocation)
				}
			}
			if ms.tryIncrementCalls != tt.wantTryIncCalls {
				t.Fatalf("TryIncrementClick calls = %d, want %d", ms.tryIncrementCalls, tt.wantTryIncCalls)
			}
		})
	}
}
