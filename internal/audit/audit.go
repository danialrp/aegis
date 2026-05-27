// SPDX-License-Identifier: AGPL-3.0-or-later

// Package audit is a thin typed wrapper around the audit_log table.
// Domain packages assemble Event values and call Record; the package
// owns the conversion to sqlc.CreateAuditEventParams and the JSON
// marshalling of the payload.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/danialrp/aegis/internal/db/sqlc"
)

// Event describes a single audit entry. All pointer fields are
// optional and stored as NULL when nil.
type Event struct {
	ActorUserID *int64
	ActorIP     *netip.Addr
	Action      string
	TargetType  string
	TargetID    *int64
	Payload     any
}

// Recorder writes Events to the audit_log table.
type Recorder struct {
	q *sqlc.Queries
}

// New builds a Recorder over the given sqlc query handle.
func New(q *sqlc.Queries) *Recorder {
	return &Recorder{q: q}
}

// Record persists e. The Payload is JSON-marshalled before insertion;
// pass nil to leave it unset.
func (r *Recorder) Record(ctx context.Context, e Event) error {
	params := sqlc.CreateAuditEventParams{Action: e.Action}

	if e.ActorUserID != nil {
		params.ActorUserID = pgtype.Int8{Int64: *e.ActorUserID, Valid: true}
	}
	if e.ActorIP != nil {
		params.ActorIp = e.ActorIP
	}
	if e.TargetType != "" {
		params.TargetType = pgtype.Text{String: e.TargetType, Valid: true}
	}
	if e.TargetID != nil {
		params.TargetID = pgtype.Int8{Int64: *e.TargetID, Valid: true}
	}
	if e.Payload != nil {
		b, err := json.Marshal(e.Payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		params.Payload = b
	}

	_, err := r.q.CreateAuditEvent(ctx, params)
	if err != nil {
		return fmt.Errorf("insert audit_log: %w", err)
	}
	return nil
}
