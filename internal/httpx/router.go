package httpx

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	pq "github.com/lib/pq"

	"github.com/example/shorty/internal/config"
	"github.com/example/shorty/internal/meta"
	"github.com/example/shorty/internal/shortid"
	"github.com/example/shorty/internal/store"
	"github.com/skip2/go-qrcode"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	metricLinksCreated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "shorty_links_created_total",
		Help: "Total number of short links successfully created.",
	})
	metricRedirects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "shorty_resolve_redirects_total",
		Help: "Total number of successful redirects to long URLs.",
	})
	metricCollectErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "shorty_metrics_collect_errors_total",
		Help: "Number of errors while collecting Prometheus metrics.",
	})
)

type Handlers struct {
	cfg        config.Config
	store      store.Store
	tplIndex   *template.Template
	tplPreview *template.Template
	staticFS   http.FileSystem
}

func NewHandlers(cfg config.Config, st store.Store, efs fs.FS) (*Handlers, error) {
	tplFS, err := fs.Sub(efs, "templates")
	if err != nil {
		return nil, err
	}
	static, err := fs.Sub(efs, "static")
	if err != nil {
		return nil, err
	}

	idx, err := template.ParseFS(tplFS, "index.html")
	if err != nil {
		return nil, err
	}
	prev, err := template.ParseFS(tplFS, "preview.html")
	if err != nil {
		return nil, err
	}

	h := &Handlers{
		cfg:        cfg,
		store:      st,
		tplIndex:   idx,
		tplPreview: prev,
		staticFS:   http.FS(static),
	}

	// ---- Prometheus GaugeFuncs (werden bei jedem Scrape berechnet) ----
	// Helper: kapselt Zähl-Queries mit Timeout + Fehlersignal
	withCount := func(f func(context.Context) (int64, error)) func() float64 {
		return func() float64 {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			n, err := f(ctx)
			if err != nil {
				metricCollectErrors.Inc()
				return math.NaN() // zeigt "no value" an; alternativ 0
			}
			return float64(n)
		}
	}

	prometheus.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "shorty_links_total",
			Help: "Total number of links in the database.",
		}, withCount(st.CountLinks)),
	)

	prometheus.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "shorty_links_active",
			Help: "Number of currently active links (not expired and not maxed).",
		}, withCount(st.CountActiveLinks)),
	)

	prometheus.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "shorty_tags_distinct_total",
			Help: "Number of distinct tags across all links.",
		}, withCount(st.CountDistinctTags)),
	)

	prometheus.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "shorty_clicks_sum",
			Help: "Sum of click_count across all links.",
		}, withCount(st.SumClicks)),
	)

	return h, nil
}

func (h *Handlers) Mount(r *chi.Mux) {
	// Health & Metrics
	r.Get("/healthz", h.Healthz)
	r.Handle("/metrics", promhttp.Handler())

	// Public auth + static routes
	r.Get("/auth/login", h.AuthLogin)
	r.Get("/auth/callback", h.AuthCallback)
	r.Get("/auth/logout", h.AuthLogout)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(h.staticFS)))

	// Protected UI + API routes
	r.Group(func(pr chi.Router) {
		pr.Use(h.RequireAuth)
		pr.Get("/", h.Index)
		pr.Post("/api/links", h.API_CreateLink)
		pr.Get("/api/links", h.API_ListLinks)
		pr.Get("/api/links/{code}", h.API_GetLink)
		pr.Get("/api/links/{code}/stats", h.API_GetStats)
		pr.Get("/api/links/{code}/qr.png", h.API_QR)
	})

	// Public resolve/preview routes
	r.Get("/preview/{code}", h.Preview)
	r.Get("/{code}", h.Resolve)
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	_ = h.tplIndex.Execute(w, map[string]any{"BaseURL": h.cfg.BaseURL})
}

type createIn struct {
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expires_at"`
	MaxClicks *int64     `json:"max_clicks"`
}

func (h *Handlers) API_CreateLink(w http.ResponseWriter, r *http.Request) {
	var in createIn
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	in.URL = strings.TrimSpace(in.URL)
	if in.URL == "" || !strings.HasPrefix(in.URL, "http") {
		http.Error(w, "url required (http/https)", http.StatusBadRequest)
		return
	}

	log.Printf("API_CreateLink begin url=%s expires=%v max=%v", in.URL, in.ExpiresAt, in.MaxClicks)

	// De-duplicate: if an active short link for this URL exists, return it
	if existing, err := h.store.FindActiveByLongURL(r.Context(), in.URL); err == nil {
		log.Printf("API_CreateLink dedupe hit code=%s for url=%s", existing.Code, in.URL)
		resp := map[string]any{
			"code":        existing.Code,
			"short_url":   strings.TrimRight(h.cfg.BaseURL, "/") + "/" + existing.Code,
			"preview_url": strings.TrimRight(h.cfg.BaseURL, "/") + "/preview/" + existing.Code,
			"qr_url":      strings.TrimRight(h.cfg.BaseURL, "/") + "/api/links/" + existing.Code + "/qr.png",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// initial tags from URL host (quick)
	initTags := []string{}
	if u, err := http.NewRequest(http.MethodGet, in.URL, nil); err == nil {
		host := u.URL.Hostname()
		if host != "" {
			parts := strings.Split(host, ".")
			if len(parts) >= 2 {
				initTags = append(initTags, strings.ToLower(parts[len(parts)-2]))
			} else {
				initTags = append(initTags, strings.ToLower(host))
			}
		}
	}

	// random code
	code := ""
	for i := 0; i < 10; i++ {
		c, err := shortid.Random(7)
		if err != nil {
			continue
		}
		if _, err := h.store.GetLink(r.Context(), c); errors.Is(err, store.ErrNotFound) {
			code = c
			break
		}
	}
	if code == "" {
		http.Error(w, "cannot allocate code", http.StatusInternalServerError)
		return
	}

	L := store.Link{
		Code:      code,
		LongURL:   in.URL,
		CreatedAt: time.Now().UTC(),
		IsActive:  true,
		Custom:    false,
		Tags:      initTags,
	}
	if in.ExpiresAt != nil {
		L.ExpiresAt = sql.NullTime{Valid: true, Time: *in.ExpiresAt}
	}
	if in.MaxClicks != nil {
		L.MaxClicks = sql.NullInt64{Valid: true, Int64: *in.MaxClicks}
	}

	if err := h.store.CreateLink(r.Context(), L); err != nil {
		// explicit console log
		var pqe *pq.Error
		if errors.As(err, &pqe) {
			log.Printf("CreateLink pq error: code=%s severity=%s message=%s detail=%s where=%s",
				string(pqe.Code), pqe.Severity, pqe.Message, pqe.Detail, pqe.Where)
		} else {
			log.Printf("CreateLink error: %T: %v", err, err)
		}

		if errors.Is(err, store.ErrCodeTaken) {
			http.Error(w, "code conflict", http.StatusConflict)
			return
		}
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	// increment metric after successful creation
	metricLinksCreated.Inc()

	// fetch meta in background (and infer tags)
	go func(ctx context.Context, code string, linkURL string) {
		mctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		md, err := meta.Fetch(mctx, linkURL, 5*time.Second)
		if err == nil {
			md.Tags = meta.InferTags(linkURL, md)
			if err := h.store.UpdateMeta(context.Background(), code, md); err != nil {
				log.Printf("update meta failed for %s: %v", code, err)
			}
		} else {
			log.Printf("meta fetch failed for %s: %v", linkURL, err)
		}
	}(context.Background(), code, L.LongURL)

	resp := map[string]any{
		"code":        code,
		"short_url":   strings.TrimRight(h.cfg.BaseURL, "/") + "/" + code,
		"preview_url": strings.TrimRight(h.cfg.BaseURL, "/") + "/preview/" + code,
		"qr_url":      strings.TrimRight(h.cfg.BaseURL, "/") + "/api/links/" + code + "/qr.png",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) API_ListLinks(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")

	var (
		links []store.Link
		err   error
	)
	if tag != "" {
		links, err = h.store.ListLinksByTag(r.Context(), tag, 50)
	} else {
		links, err = h.store.ListLinks(r.Context(), 50)
	}
	if err != nil {
		log.Printf("API_ListLinks db error: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(links)
}

func (h *Handlers) API_GetLink(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	L, err := h.store.GetLink(r.Context(), code)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(L)
}

func (h *Handlers) API_GetStats(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	st, err := h.store.GetStats(r.Context(), code)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

func (h *Handlers) API_QR(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	_, err := h.store.GetLink(r.Context(), code)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	url := strings.TrimRight(h.cfg.BaseURL, "/") + "/" + code
	png, err := qrcode.Encode(url, qrcode.Medium, 256)
	if err != nil {
		log.Printf("API_QR encode error: %v", err)
		http.Error(w, "qr error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

func (h *Handlers) Preview(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	L, err := h.store.GetLink(r.Context(), code)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !L.IsActive || (L.ExpiresAt.Valid && time.Now().After(L.ExpiresAt.Time)) {
		http.NotFound(w, r)
		return
	}
	if L.MaxClicks.Valid && L.ClickCount >= L.MaxClicks.Int64 {
		http.NotFound(w, r)
		return
	}
	_ = h.tplPreview.Execute(w, map[string]any{"BaseURL": h.cfg.BaseURL, "L": L})
}

func (h *Handlers) Resolve(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	L, err := h.store.GetLink(r.Context(), code)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if !L.IsActive || (L.ExpiresAt.Valid && time.Now().After(L.ExpiresAt.Time)) {
		http.NotFound(w, r)
		return
	}
	if L.MaxClicks.Valid && L.ClickCount >= L.MaxClicks.Int64 {
		http.NotFound(w, r)
		return
	}

	ua := strings.ToLower(r.UserAgent())
	if looksLikeBot(ua) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		_ = h.tplPreview.Execute(w, map[string]any{"BaseURL": h.cfg.BaseURL, "L": L})
		return
	}

	allowed, err := h.store.TryIncrementClick(r.Context(), code, r.UserAgent(), r.Referer(), clientCountry(r))
	if err != nil {
		log.Printf("Resolve/TryIncrementClick db error: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.NotFound(w, r)
		return
	}

	// metric: successful redirect
	metricRedirects.Inc()

	http.Redirect(w, r, L.LongURL, http.StatusFound)
}

func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
	defer cancel()
	if err := h.store.Ping(ctx); err != nil {
		http.Error(w, "db=down", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func looksLikeBot(ua string) bool {
	bots := []string{"slackbot", "twitterbot", "facebookexternalhit", "linkedinbot", "discordbot", "telegrambot", "whatsapp"}
	for _, b := range bots {
		if strings.Contains(ua, b) {
			return true
		}
	}
	return false
}

func clientCountry(r *http.Request) string {
	if c := r.Header.Get("CF-IPCountry"); len(c) == 2 {
		return strings.ToUpper(c)
	}
	return ""
}
