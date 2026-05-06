package store

import (
	"context"
	"database/sql"
	"time"
)

var (
	ErrNotFound  = sql.ErrNoRows
	ErrCodeTaken = &ErrConflict{Msg: "code already exists"}
)

type ErrConflict struct{ Msg string }

func (e *ErrConflict) Error() string { return e.Msg }

type Link struct {
	Code        string         `json:"code"`
	LongURL     string         `json:"long_url"`
	Title       sql.NullString `json:"title"`
	Description sql.NullString `json:"description"`
	ImageURL    sql.NullString `json:"image_url"`
	SiteName    sql.NullString `json:"site_name"`
	CreatedAt   time.Time      `json:"created_at"`
	ExpiresAt   sql.NullTime   `json:"expires_at"`
	IsActive    bool           `json:"is_active"`
	MaxClicks   sql.NullInt64  `json:"max_clicks"`
	ClickCount  int64          `json:"click_count"`
	Custom      bool           `json:"custom"`
	Tags        []string       `json:"tags"`
}

type Stats struct {
	Code         string        `json:"code"`
	ClickCount   int64         `json:"click_count"`
	ClicksPerDay []DailyClicks `json:"clicks_per_day"`
	TopReferrers []ReferrerHit `json:"top_referrers"`
	UserAgentMix []UAClassHit  `json:"user_agent_mix"`
}

type DailyClicks struct {
	Date   string `json:"date"`
	Clicks int64  `json:"clicks"`
}

type ReferrerHit struct {
	Referrer string `json:"referrer"`
	Clicks   int64  `json:"clicks"`
}

type UAClassHit struct {
	Class  string `json:"class"`
	Clicks int64  `json:"clicks"`
}

type Meta struct {
	Title       string
	Description string
	ImageURL    string
	SiteName    string
	Tags        []string
}

type Store interface {
	CreateLink(ctx context.Context, l Link) error
	GetLink(ctx context.Context, code string) (Link, error)
	ListLinks(ctx context.Context, limit int) ([]Link, error)
	IncrementClick(ctx context.Context, code, ua, referer, country string) error
	TryIncrementClick(ctx context.Context, code, ua, referer, country string) (bool, error)
	FindActiveByLongURL(ctx context.Context, longURL string) (Link, error)
	ListLinksByTag(ctx context.Context, tag string, limit int) ([]Link, error)
	GetStats(ctx context.Context, code string) (Stats, error)
	UpdateMeta(ctx context.Context, code string, md Meta) error
	Ping(ctx context.Context) error
	CountLinks(ctx context.Context) (int64, error)
	CountActiveLinks(ctx context.Context) (int64, error)
	CountDistinctTags(ctx context.Context) (int64, error)
	SumClicks(ctx context.Context) (int64, error)
}
