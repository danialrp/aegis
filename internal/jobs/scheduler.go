// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/robfig/cron/v3"

	"github.com/danialrp/aegis/internal/db/sqlc"
)

// CronScheduler watches site_deploy_scripts for cron_spec values and
// enqueues scheduled deploys.
//
// Design: every refreshInterval, we read every script row with a
// non-empty cron_spec, rebuild the in-memory schedule from scratch,
// and let the underlying cron lib own the timing. Cheap, robust, and
// the operator's edits in the UI take effect within one tick of the
// refresh.
type CronScheduler struct {
	queries     *sqlc.Queries
	riverClient *river.Client[pgx.Tx]
	logger      *slog.Logger

	refreshInterval time.Duration

	mu  sync.Mutex
	c   *cron.Cron
	ids map[int64]cron.EntryID // site_id → current entry
}

// NewCronScheduler builds a scheduler. Default refresh interval is
// 30 seconds — small enough that "save script" feels live, large
// enough to avoid hammering the DB.
func NewCronScheduler(q *sqlc.Queries, rc *river.Client[pgx.Tx], logger *slog.Logger) *CronScheduler {
	return &CronScheduler{
		queries:         q,
		riverClient:     rc,
		logger:          logger,
		refreshInterval: 30 * time.Second,
		c:               cron.New(),
		ids:             make(map[int64]cron.EntryID),
	}
}

// Run starts the scheduler loop. Blocks until ctx is cancelled.
func (s *CronScheduler) Run(ctx context.Context) error {
	s.c.Start()
	defer func() {
		<-s.c.Stop().Done()
	}()

	// Sync immediately, then on a ticker.
	if err := s.sync(ctx); err != nil {
		s.logger.Warn("initial cron sync failed", "err", err)
	}

	tick := time.NewTicker(s.refreshInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("cron scheduler stopping")
			return ctx.Err()
		case <-tick.C:
			if err := s.sync(ctx); err != nil {
				s.logger.Warn("cron sync failed", "err", err)
			}
		}
	}
}

// sync rebuilds the active entry set from site_deploy_scripts.
func (s *CronScheduler) sync(ctx context.Context) error {
	rows, err := s.queries.ListScheduledDeployScripts(ctx)
	if err != nil {
		return fmt.Errorf("list scheduled scripts: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build the desired set; remove anything stale.
	wanted := make(map[int64]string, len(rows))
	for _, r := range rows {
		if r.CronSpec.Valid && r.CronSpec.String != "" {
			wanted[r.SiteID] = r.CronSpec.String
		}
	}

	// Drop entries no longer wanted or whose spec changed.
	for siteID, entryID := range s.ids {
		want, ok := wanted[siteID]
		if !ok {
			s.c.Remove(entryID)
			delete(s.ids, siteID)
			continue
		}
		// Cheap check: compare entry's schedule string by re-adding
		// when in doubt. We always rebuild changed entries.
		if !s.entryHasSpec(entryID, want) {
			s.c.Remove(entryID)
			delete(s.ids, siteID)
		}
	}

	// Add anything missing.
	for siteID, spec := range wanted {
		if _, ok := s.ids[siteID]; ok {
			continue
		}
		id, err := s.c.AddFunc(spec, s.tickerFor(siteID))
		if err != nil {
			s.logger.Warn("invalid cron spec",
				"site_id", siteID, "spec", spec, "err", err)
			continue
		}
		s.ids[siteID] = id
	}
	return nil
}

// entryHasSpec is intentionally pessimistic: robfig/cron's Entry does
// not expose the original spec string, so we never claim "still
// matches" — we always rebuild. (Cheap enough; ~tens of entries max.)
func (s *CronScheduler) entryHasSpec(cron.EntryID, string) bool { return false }

// tickerFor returns the closure scheduled for siteID. Called from the
// cron goroutine. Uses a background context with a generous timeout
// since the schedule outlives any single Run() call.
func (s *CronScheduler) tickerFor(siteID int64) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		row, err := s.queries.CreateDeploy(ctx, sqlc.CreateDeployParams{
			SiteID: siteID, Trigger: "schedule",
		})
		if err != nil {
			s.logger.Warn("scheduled deploy create failed",
				"site_id", siteID, "err", err)
			return
		}
		if _, err := s.riverClient.Insert(ctx, RunDeployArgs{DeployID: row.ID}, nil); err != nil {
			s.logger.Warn("scheduled deploy enqueue failed",
				"site_id", siteID, "deploy_id", row.ID, "err", err)
			return
		}
		s.logger.Info("scheduled deploy queued",
			"site_id", siteID, "deploy_id", row.ID)
	}
}
