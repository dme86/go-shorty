package meta

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/example/shorty/internal/store"
)

// Fetch reads public HTTP(S) metadata only. DNS is validated and pinned at
// dial time (including on redirects), preventing access to private services.
func Fetch(ctx context.Context, target string, timeout time.Duration) (store.Meta, error) {
	u, err := neturl.Parse(target)
	if err != nil {
		return store.Meta{}, err
	}
	if err := validateMetadataURL(u); err != nil {
		return store.Meta{}, err
	}

	client := newMetadataClient(timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return store.Meta{}, err
	}
	req.Header.Set("User-Agent", "ShortyMetaBot/1.0 (+https://example.com)")

	resp, err := client.Do(req)
	if err != nil {
		return store.Meta{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return store.Meta{}, fmt.Errorf("metadata fetch returned HTTP %d", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || (mediaType != "text/html" && mediaType != "application/xhtml+xml") {
			return store.Meta{}, fmt.Errorf("metadata response is not HTML")
		}
	}

	const maxHTMLBytes = 2 << 20
	html, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLBytes+1))
	if err != nil {
		return store.Meta{}, err
	}
	if len(html) > maxHTMLBytes {
		return store.Meta{}, fmt.Errorf("metadata HTML exceeds 2 MiB limit")
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return store.Meta{}, err
	}

	get := func(sel, attr string) string {
		if s := doc.Find(sel).First(); s.Length() > 0 {
			if v, ok := s.Attr(attr); ok {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}

	md := store.Meta{
		Title:       get(`meta[property='og:title']`, "content"),
		Description: get(`meta[property='og:description']`, "content"),
		ImageURL:    get(`meta[property='og:image']`, "content"),
		SiteName:    get(`meta[property='og:site_name']`, "content"),
	}
	if md.Title == "" {
		md.Title = strings.TrimSpace(doc.Find("title").Text())
	}
	if md.Description == "" {
		md.Description = get(`meta[name='description']`, "content")
	}
	return md, nil
}

// InferTags: Host + Pfad + Query + Keywords aus Meta → Tags (max 8).
func InferTags(target string, md store.Meta) []string {
	tags := map[string]struct{}{}

	if u, err := neturl.Parse(target); err == nil && u != nil {
		for _, t := range hostTags(u.Hostname()) {
			tags[t] = struct{}{}
		}
		for _, t := range pathTags(u.Path) {
			tags[t] = struct{}{}
		}
		for _, t := range queryTags(u.Query()) {
			tags[t] = struct{}{}
		}
	}

	for _, w := range keywords(md.Title) {
		tags[w] = struct{}{}
	}
	for _, w := range keywords(md.Description) {
		tags[w] = struct{}{}
	}

	out := make([]string, 0, len(tags))
	for k := range tags {
		out = append(out, k)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func hostTags(host string) []string {
	host = strings.ToLower(host)
	parts := strings.Split(host, ".")
	base := host
	if len(parts) >= 2 {
		base = parts[len(parts)-2]
	}

	var out []string
	switch {
	case strings.Contains(host, "steamcommunity") || strings.Contains(host, "steampowered"):
		out = append(out, "steam")
	case strings.Contains(host, "youtube") || strings.Contains(host, "youtu.be"):
		out = append(out, "youtube", "video")
	case strings.Contains(host, "github"):
		out = append(out, "github", "code")
	case strings.Contains(host, "x.com") || strings.Contains(host, "twitter"):
		out = append(out, "twitter", "social")
	case strings.Contains(host, "tiktok"):
		out = append(out, "tiktok", "video")
	case strings.Contains(host, "medium.com"):
		out = append(out, "article")
	case strings.Contains(host, "docs.google.com"):
		out = append(out, "google-docs")
	}

	if len(out) == 0 {
		out = append(out, base)
	}
	return out
}

func pathTags(path string) []string {
	path = strings.ToLower(path)
	repl := strings.NewReplacer("/", " ", "-", " ", "_", " ")
	parts := strings.Fields(repl.Replace(path))

	var out []string
	if strings.Contains(path, "/sharedfiles") || strings.Contains(path, "workshop") {
		out = append(out, "workshop")
	}

	stop := map[string]struct{}{
		"user": {}, "profile": {}, "file": {}, "files": {}, "details": {},
		"item": {}, "id": {}, "page": {}, "status": {},
	}
	for _, p := range parts {
		if len(p) < 4 {
			continue
		}
		if _, bad := stop[p]; bad {
			continue
		}
		if p == "sharedfiles" || p == "filedetails" || p == "workshop" {
			out = append(out, p)
		}
	}
	return dedupe(out)
}

func queryTags(v neturl.Values) []string {
	keys := []string{"q", "query", "search", "searchtext", "tags"}
	var out []string
	for _, k := range keys {
		val := strings.TrimSpace(v.Get(k))
		if val == "" {
			continue
		}
		for _, w := range keywords(val) {
			out = append(out, w)
		}
	}
	return dedupe(out)
}

func keywords(s string) []string {
	s = strings.ToLower(s)
	repl := strings.NewReplacer(
		",", " ", ".", " ", ":", " ", ";", " ",
		"!", " ", "?", " ", "(", " ", ")", " ",
		"\"", " ", "'", " ", "\n", " ", "\t", " ",
		"/", " ", "-", " ", "_", " ", "|", " ",
	)
	s = repl.Replace(s)
	words := strings.Fields(s)

	stop := map[string]struct{}{
		"the": {}, "and": {}, "oder": {}, "und": {}, "mit": {}, "für": {},
		"ein": {}, "einer": {}, "eine": {}, "von": {}, "zur": {}, "zum": {},
		"der": {}, "die": {}, "das": {}, "http": {}, "https": {}, "www": {},
	}

	out := make([]string, 0, 6)
	for _, w := range words {
		if len(w) < 4 {
			continue
		}
		if _, bad := stop[w]; bad {
			continue
		}
		clean := strings.Trim(w, " .,_-!?:;\"'()[]{}")
		if clean == "" {
			continue
		}
		out = append(out, clean)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func dedupe(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
