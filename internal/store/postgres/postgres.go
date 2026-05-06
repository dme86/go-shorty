package postgres

import (
	"context"
	"database/sql"
	"net/url"
	"strings"

	pq "github.com/lib/pq"

	"github.com/example/shorty/internal/store"
)

type PG struct{ db *sql.DB }

func New(db *sql.DB) *PG { return &PG{db: db} }

func (p *PG) CreateLink(ctx context.Context, tenantID string, l store.Link) error {
	_, err := p.db.ExecContext(ctx, `
        INSERT INTO links (tenant_id, code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags)
        VALUES ($1,$2,$3,NULL,NULL,NULL,NULL, NOW(), $4, $5, $6, 0, $7, $8)`,
		tenantID, l.Code, l.LongURL, l.ExpiresAt, l.IsActive, l.MaxClicks, l.Custom, pq.Array(l.Tags),
	)
	if err != nil {
		if isUnique(err) {
			return store.ErrCodeTaken
		}
		return err
	}
	return nil
}

// Health ping
func (p *PG) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// Total links in DB
func (p *PG) CountLinks(ctx context.Context) (int64, error) {
	var n int64
	err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM links`).Scan(&n)
	return n, err
}

// Active links (not expired, not maxed, is_active)
func (p *PG) CountActiveLinks(ctx context.Context) (int64, error) {
	var n int64
	err := p.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM links
		WHERE is_active = TRUE
		  AND (expires_at IS NULL OR now() < expires_at)
		  AND (max_clicks IS NULL OR click_count < max_clicks)
	`).Scan(&n)
	return n, err
}

// Distinct tags across all links
func (p *PG) CountDistinctTags(ctx context.Context) (int64, error) {
	var n int64
	// COUNT DISTINCT über UNNEST
	err := p.db.QueryRowContext(ctx, `
		SELECT COALESCE(COUNT(DISTINCT t), 0)
		FROM (SELECT UNNEST(tags) AS t FROM links) u
	`).Scan(&n)
	return n, err
}

// Sum of click_count
func (p *PG) SumClicks(ctx context.Context) (int64, error) {
	var n int64
	err := p.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(click_count),0) FROM links`).Scan(&n)
	return n, err
}

func (p *PG) GetLink(ctx context.Context, tenantID, code string) (store.Link, error) {
	var l store.Link
	err := p.db.QueryRowContext(ctx, `
        SELECT tenant_id, code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags
        FROM links WHERE tenant_id=$1 AND code=$2`, tenantID, code).
		Scan(&l.TenantID, &l.Code, &l.LongURL, &l.Title, &l.Description, &l.ImageURL, &l.SiteName, &l.CreatedAt, &l.ExpiresAt, &l.IsActive, &l.MaxClicks, &l.ClickCount, &l.Custom, pq.Array(&l.Tags))
	return l, err
}

func (p *PG) GetLinkByCode(ctx context.Context, code string) (store.Link, error) {
	var l store.Link
	err := p.db.QueryRowContext(ctx, `
        SELECT tenant_id, code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags
        FROM links WHERE code=$1`, code).
		Scan(&l.TenantID, &l.Code, &l.LongURL, &l.Title, &l.Description, &l.ImageURL, &l.SiteName, &l.CreatedAt, &l.ExpiresAt, &l.IsActive, &l.MaxClicks, &l.ClickCount, &l.Custom, pq.Array(&l.Tags))
	return l, err
}

func (p *PG) ListLinks(ctx context.Context, tenantID string, limit int) ([]store.Link, error) {
	rows, err := p.db.QueryContext(ctx, `
        SELECT tenant_id, code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags
        FROM links
        WHERE tenant_id = $1
          AND is_active = TRUE
          AND (expires_at IS NULL OR now() < expires_at)
          AND (max_clicks IS NULL OR click_count < max_clicks)
        ORDER BY created_at DESC
        LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := []store.Link{}
	for rows.Next() {
		var l store.Link
		if err := rows.Scan(&l.TenantID, &l.Code, &l.LongURL, &l.Title, &l.Description, &l.ImageURL, &l.SiteName, &l.CreatedAt, &l.ExpiresAt, &l.IsActive, &l.MaxClicks, &l.ClickCount, &l.Custom, pq.Array(&l.Tags)); err != nil {
			return nil, err
		}
		res = append(res, l)
	}
	return res, rows.Err()
}

func (p *PG) TryIncrementClick(ctx context.Context, tenantID, code, ua, referer, country string) (bool, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	// Atomically enforce max_clicks, expiry, active
	var ok bool
	err = tx.QueryRowContext(ctx, `
        UPDATE links
        SET click_count = click_count + 1
        WHERE tenant_id=$1
          AND code=$2
          AND is_active = TRUE
          AND (expires_at IS NULL OR now() < expires_at)
          AND (max_clicks IS NULL OR click_count < max_clicks)
        RETURNING true
    `, tenantID, code).Scan(&ok)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO clicks (tenant_id, code, ua, referer, country) VALUES ($1,$2,$3,$4,$5)`, tenantID, code, ua, referer, country); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (p *PG) IncrementClick(ctx context.Context, tenantID, code, ua, referer, country string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `UPDATE links SET click_count=click_count+1 WHERE tenant_id=$1 AND code=$2`, tenantID, code); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO clicks (tenant_id, code, ua, referer, country) VALUES ($1,$2,$3,$4,$5)`, tenantID, code, ua, referer, country); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *PG) GetStats(ctx context.Context, tenantID, code string) (store.Stats, error) {
	var s store.Stats
	s.TenantID = tenantID
	s.Code = code
	err := p.db.QueryRowContext(ctx, `SELECT click_count FROM links WHERE tenant_id=$1 AND code=$2`, tenantID, code).Scan(&s.ClickCount)
	if err != nil {
		return s, err
	}

	dayRows, err := p.db.QueryContext(ctx, `
		SELECT to_char(date_trunc('day', ts), 'YYYY-MM-DD') AS d, COUNT(*)::bigint
		FROM clicks
		WHERE tenant_id=$1 AND code=$2
		GROUP BY d
		ORDER BY d ASC
	`, tenantID, code)
	if err != nil {
		return s, err
	}
	for dayRows.Next() {
		var d store.DailyClicks
		if err := dayRows.Scan(&d.Date, &d.Clicks); err != nil {
			dayRows.Close()
			return s, err
		}
		s.ClicksPerDay = append(s.ClicksPerDay, d)
	}
	if err := dayRows.Err(); err != nil {
		dayRows.Close()
		return s, err
	}
	dayRows.Close()

	refRows, err := p.db.QueryContext(ctx, `
		SELECT referer, COUNT(*)::bigint
		FROM clicks
		WHERE tenant_id=$1
		  AND code=$2
		  AND referer IS NOT NULL
		  AND btrim(referer) <> ''
		GROUP BY referer
		ORDER BY COUNT(*) DESC
		LIMIT 10
	`, tenantID, code)
	if err != nil {
		return s, err
	}
	for refRows.Next() {
		var raw string
		var c int64
		if err := refRows.Scan(&raw, &c); err != nil {
			refRows.Close()
			return s, err
		}
		s.TopReferrers = append(s.TopReferrers, store.ReferrerHit{
			Referrer: normalizeReferrer(raw),
			Clicks:   c,
		})
	}
	if err := refRows.Err(); err != nil {
		refRows.Close()
		return s, err
	}
	refRows.Close()

	uaRows, err := p.db.QueryContext(ctx, `
		SELECT
			CASE
				WHEN ua IS NULL OR btrim(ua) = '' THEN 'unknown'
				WHEN lower(ua) LIKE '%bot%' OR lower(ua) LIKE '%crawl%' OR lower(ua) LIKE '%spider%' OR lower(ua) LIKE '%slackbot%' OR lower(ua) LIKE '%discordbot%' OR lower(ua) LIKE '%telegrambot%' OR lower(ua) LIKE '%whatsapp%' THEN 'bot'
				WHEN lower(ua) LIKE '%mobile%' OR lower(ua) LIKE '%android%' OR lower(ua) LIKE '%iphone%' THEN 'mobile'
				ELSE 'desktop'
			END AS cls,
			COUNT(*)::bigint
		FROM clicks
		WHERE tenant_id=$1
		  AND code=$2
		GROUP BY cls
		ORDER BY COUNT(*) DESC
	`, tenantID, code)
	if err != nil {
		return s, err
	}
	for uaRows.Next() {
		var u store.UAClassHit
		if err := uaRows.Scan(&u.Class, &u.Clicks); err != nil {
			uaRows.Close()
			return s, err
		}
		s.UserAgentMix = append(s.UserAgentMix, u)
	}
	if err := uaRows.Err(); err != nil {
		uaRows.Close()
		return s, err
	}
	uaRows.Close()

	return s, nil
}

func normalizeReferrer(raw string) string {
	r := strings.TrimSpace(raw)
	if r == "" {
		return "unknown"
	}
	u, err := url.Parse(r)
	if err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	if !strings.Contains(r, "://") {
		u2, err2 := url.Parse("https://" + r)
		if err2 == nil && u2.Host != "" {
			return strings.ToLower(u2.Host)
		}
	}
	return strings.ToLower(r)
}

func (p *PG) UpdateMeta(ctx context.Context, tenantID, code string, md store.Meta) error {
	_, err := p.db.ExecContext(ctx, `
        UPDATE links SET title=$3, description=$4, image_url=$5, site_name=$6, tags=$7 WHERE tenant_id=$1 AND code=$2`,
		tenantID, code, nullable(md.Title), nullable(md.Description), nullable(md.ImageURL), nullable(md.SiteName), pq.Array(md.Tags),
	)
	return err
}

func (p *PG) FindActiveByLongURL(ctx context.Context, tenantID, longURL string) (store.Link, error) {
	var l store.Link
	err := p.db.QueryRowContext(ctx, `
        SELECT tenant_id, code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags
        FROM links
        WHERE tenant_id = $1
          AND long_url = $2
          AND is_active = TRUE
          AND (expires_at IS NULL OR now() < expires_at)
          AND (max_clicks IS NULL OR click_count < max_clicks)
        ORDER BY created_at DESC
        LIMIT 1
    `, tenantID, longURL).
		Scan(&l.TenantID, &l.Code, &l.LongURL, &l.Title, &l.Description, &l.ImageURL, &l.SiteName, &l.CreatedAt, &l.ExpiresAt, &l.IsActive, &l.MaxClicks, &l.ClickCount, &l.Custom, pq.Array(&l.Tags))
	return l, err
}

func (p *PG) ListLinksByTag(ctx context.Context, tenantID, tag string, limit int) ([]store.Link, error) {
	rows, err := p.db.QueryContext(ctx, `
        SELECT tenant_id, code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags
        FROM links
        WHERE tenant_id = $1
          AND is_active = TRUE
          AND (expires_at IS NULL OR now() < expires_at)
          AND (max_clicks IS NULL OR click_count < max_clicks)
          AND tags @> ARRAY[$2]::text[]
        ORDER BY created_at DESC
        LIMIT $3`, tenantID, tag, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := []store.Link{}
	for rows.Next() {
		var l store.Link
		if err := rows.Scan(&l.TenantID, &l.Code, &l.LongURL, &l.Title, &l.Description, &l.ImageURL, &l.SiteName, &l.CreatedAt, &l.ExpiresAt, &l.IsActive, &l.MaxClicks, &l.ClickCount, &l.Custom, pq.Array(&l.Tags)); err != nil {
			return nil, err
		}
		res = append(res, l)
	}
	return res, rows.Err()
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate key value violates unique constraint")
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
