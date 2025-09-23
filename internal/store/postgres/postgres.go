package postgres

import (
	"context"
	"database/sql"
	"strings"

	pq "github.com/lib/pq"

	"github.com/example/shorty/internal/store"
)

type PG struct{ db *sql.DB }

func New(db *sql.DB) *PG { return &PG{db: db} }

func (p *PG) CreateLink(ctx context.Context, l store.Link) error {
	_, err := p.db.ExecContext(ctx, `
        INSERT INTO links (code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags)
        VALUES ($1,$2,NULL,NULL,NULL,NULL, NOW(), $3, $4, $5, 0, $6, $7)`,
		l.Code, l.LongURL, l.ExpiresAt, l.IsActive, l.MaxClicks, l.Custom, pq.Array(l.Tags),
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

func (p *PG) GetLink(ctx context.Context, code string) (store.Link, error) {
	var l store.Link
	err := p.db.QueryRowContext(ctx, `
        SELECT code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags
        FROM links WHERE code=$1`, code).
		Scan(&l.Code, &l.LongURL, &l.Title, &l.Description, &l.ImageURL, &l.SiteName, &l.CreatedAt, &l.ExpiresAt, &l.IsActive, &l.MaxClicks, &l.ClickCount, &l.Custom, pq.Array(&l.Tags))
	return l, err
}

func (p *PG) ListLinks(ctx context.Context, limit int) ([]store.Link, error) {
	rows, err := p.db.QueryContext(ctx, `
        SELECT code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags
        FROM links
        WHERE is_active = TRUE
          AND (expires_at IS NULL OR now() < expires_at)
          AND (max_clicks IS NULL OR click_count < max_clicks)
        ORDER BY created_at DESC
        LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := []store.Link{}
	for rows.Next() {
		var l store.Link
		if err := rows.Scan(&l.Code, &l.LongURL, &l.Title, &l.Description, &l.ImageURL, &l.SiteName, &l.CreatedAt, &l.ExpiresAt, &l.IsActive, &l.MaxClicks, &l.ClickCount, &l.Custom, pq.Array(&l.Tags)); err != nil {
			return nil, err
		}
		res = append(res, l)
	}
	return res, rows.Err()
}

func (p *PG) TryIncrementClick(ctx context.Context, code, ua, referer, country string) (bool, error) {
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
        WHERE code=$1
          AND is_active = TRUE
          AND (expires_at IS NULL OR now() < expires_at)
          AND (max_clicks IS NULL OR click_count < max_clicks)
        RETURNING true
    `, code).Scan(&ok)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO clicks (code, ua, referer, country) VALUES ($1,$2,$3,$4)`, code, ua, referer, country); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (p *PG) IncrementClick(ctx context.Context, code, ua, referer, country string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `UPDATE links SET click_count=click_count+1 WHERE code=$1`, code); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO clicks (code, ua, referer, country) VALUES ($1,$2,$3,$4)`, code, ua, referer, country); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *PG) GetStats(ctx context.Context, code string) (store.Stats, error) {
	var s store.Stats
	s.Code = code
	err := p.db.QueryRowContext(ctx, `SELECT click_count FROM links WHERE code=$1`, code).Scan(&s.ClickCount)
	return s, err
}

func (p *PG) UpdateMeta(ctx context.Context, code string, md store.Meta) error {
	_, err := p.db.ExecContext(ctx, `
        UPDATE links SET title=$2, description=$3, image_url=$4, site_name=$5, tags=$6 WHERE code=$1`,
		code, nullable(md.Title), nullable(md.Description), nullable(md.ImageURL), nullable(md.SiteName), pq.Array(md.Tags),
	)
	return err
}

func (p *PG) FindActiveByLongURL(ctx context.Context, longURL string) (store.Link, error) {
	var l store.Link
	err := p.db.QueryRowContext(ctx, `
        SELECT code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags
        FROM links
        WHERE long_url = $1
          AND is_active = TRUE
          AND (expires_at IS NULL OR now() < expires_at)
          AND (max_clicks IS NULL OR click_count < max_clicks)
        ORDER BY created_at DESC
        LIMIT 1
    `, longURL).
		Scan(&l.Code, &l.LongURL, &l.Title, &l.Description, &l.ImageURL, &l.SiteName, &l.CreatedAt, &l.ExpiresAt, &l.IsActive, &l.MaxClicks, &l.ClickCount, &l.Custom, pq.Array(&l.Tags))
	return l, err
}

func (p *PG) ListLinksByTag(ctx context.Context, tag string, limit int) ([]store.Link, error) {
	rows, err := p.db.QueryContext(ctx, `
        SELECT code, long_url, title, description, image_url, site_name, created_at, expires_at, is_active, max_clicks, click_count, custom, tags
        FROM links
        WHERE is_active = TRUE
          AND (expires_at IS NULL OR now() < expires_at)
          AND (max_clicks IS NULL OR click_count < max_clicks)
          AND tags @> ARRAY[$1]::text[]
        ORDER BY created_at DESC
        LIMIT $2`, tag, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := []store.Link{}
	for rows.Next() {
		var l store.Link
		if err := rows.Scan(&l.Code, &l.LongURL, &l.Title, &l.Description, &l.ImageURL, &l.SiteName, &l.CreatedAt, &l.ExpiresAt, &l.IsActive, &l.MaxClicks, &l.ClickCount, &l.Custom, pq.Array(&l.Tags)); err != nil {
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
