package postgres

import (
	"context"
	"errors"
	"log"
	"time"
)

// DeleteOldClicks deletes at most batch raw events older than cutoff.
// links.click_count (the lifetime total) is never modified.
func (p *PG) DeleteOldClicks(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	if batch <= 0 || batch > 10000 {
		return 0, errors.New("click cleanup batch must be between 1 and 10000")
	}
	result, err := p.db.ExecContext(ctx, `
		WITH expired AS (
			SELECT id FROM clicks
			WHERE ts < $1
			ORDER BY ts, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM clicks c USING expired
		WHERE c.id = expired.id
	`, cutoff, batch)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// RunClickRetention starts with one cleanup pass, then checks hourly. The
// batch size caps transaction work; sleeping between batches gives other
// database operations room to progress, including under large backlogs.
func RunClickRetention(ctx context.Context, st *PG, days, batch int) {
	if days <= 0 {
		days = 90
	}
	if days > 3650 {
		days = 3650
	}
	if batch <= 0 {
		batch = 5000
	}
	if batch > 10000 {
		batch = 10000
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	cleanup := func() {
		cutoff := time.Now().UTC().AddDate(0, 0, -days)
		for ctx.Err() == nil {
			batchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			deleted, err := st.DeleteOldClicks(batchCtx, cutoff, batch)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("click retention cleanup failed: %v", err)
				}
				return
			}
			if deleted < int64(batch) {
				return
			}
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}

	cleanup()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
