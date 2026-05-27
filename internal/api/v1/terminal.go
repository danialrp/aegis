// SPDX-License-Identifier: AGPL-3.0-or-later

package v1

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/danialrp/aegis/internal/agentbus"
	"github.com/danialrp/aegis/internal/api/middleware"
	"github.com/danialrp/aegis/internal/audit"
	"github.com/danialrp/aegis/internal/db/sqlc"
	"github.com/danialrp/aegis/pkg/protocol"
)

// TerminalHandler bridges the browser's xterm.js WebSocket and the
// agent's PTY stream.
//
// Auth: an authenticated POST /v1/sites/{id}/terminal/ticket returns
// a one-time, 60-second-valid ticket. The browser then opens the
// WS endpoint with ?ticket=<value>; this avoids putting JWTs into
// URLs (and therefore into server access logs + browser history).
type TerminalHandler struct {
	queries  *sqlc.Queries
	auditRec *audit.Recorder
	hub      *agentbus.Hub
	logger   *slog.Logger

	tickets *ticketStore
}

// NewTerminalHandler builds the handler.
func NewTerminalHandler(q *sqlc.Queries, a *audit.Recorder, hub *agentbus.Hub, logger *slog.Logger) *TerminalHandler {
	return &TerminalHandler{
		queries: q, auditRec: a, hub: hub, logger: logger,
		tickets: newTicketStore(),
	}
}

// Ticket handles POST /v1/sites/{id}/terminal/ticket.
func (h *TerminalHandler) Ticket(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	user, _ := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// Verify the site exists and pull its server id.
	site, err := h.queries.GetSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "site_not_found")
		return
	}
	if _, ok := h.hub.Get(site.ServerID); !ok {
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}
	ticket, err := h.tickets.issue(siteID, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":      ticket,
		"expires_sec": ticketTTL.Seconds(),
	})
}

// Connect handles WS upgrade at /v1/sites/{id}/terminal?ticket=...
// The WS lifetime is independent of r.Context: the HTTP request
// finishes when Accept returns, and the WS itself outlives the
// handler invocation, so we use a fresh background context.
//
//nolint:contextcheck // background ctx is intentional; see comment above
func (h *TerminalHandler) Connect(w http.ResponseWriter, r *http.Request) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		writeError(w, http.StatusUnauthorized, "missing_ticket")
		return
	}
	claim, ok := h.tickets.consume(ticket)
	if !ok || claim.siteID != siteID {
		writeError(w, http.StatusUnauthorized, "invalid_ticket")
		return
	}

	site, err := h.queries.GetSite(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "site_not_found")
		return
	}
	conn, ok := h.hub.Get(site.ServerID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "agent_offline")
		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Only allow browsers on the same origin — we're not exposing
		// this to third-party JS.
		OriginPatterns: []string{r.Host},
	})
	if err != nil {
		h.logger.Warn("terminal ws accept failed", "err", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

	openCtx, openCancel := context.WithTimeout(ctx, 10*time.Second)
	stream, err := conn.OpenStream(openCtx, protocol.MethodHostPtyOpen, protocol.PtyOpenParams{
		SiteID: site.ID,
		Cols:   80,
		Rows:   24,
	})
	openCancel()
	if err != nil {
		h.logger.Warn("pty open failed", "site_id", site.ID, "err", err)
		_ = ws.Close(websocket.StatusInternalError, "pty open failed")
		return
	}
	defer func() { _ = stream.Close() }()

	uid := claim.userID
	h.auditf(ctx, &uid, siteID, "site.terminal.opened", nil)

	// Pump bytes: ws → stream (input), stream → ws (output).
	errCh := make(chan error, 2)
	go func() { errCh <- pumpWStoStream(ctx, ws, stream) }()
	go func() { errCh <- pumpStreamToWS(ctx, stream, ws) }()
	<-errCh
}

func pumpWStoStream(ctx context.Context, ws *websocket.Conn, s *agentbus.Stream) error {
	for {
		var frame struct {
			Data   string `json:"data,omitempty"`
			Resize *resz  `json:"resize,omitempty"`
		}
		if err := wsjson.Read(ctx, ws, &frame); err != nil {
			return err
		}
		if frame.Resize != nil {
			// Resize-via-PTY would need a separate RPC; v1 of the
			// terminal opens at 80x24 and ignores resize. Plumbing
			// PTY resize is a small follow-up.
			continue
		}
		if frame.Data == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(frame.Data)
		if err != nil {
			continue
		}
		if err := s.Send(ctx, decoded); err != nil {
			return err
		}
	}
}

func pumpStreamToWS(ctx context.Context, s *agentbus.Stream, ws *websocket.Conn) error {
	for {
		b, err := s.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		enc := base64.StdEncoding.EncodeToString(b)
		if err := wsjson.Write(ctx, ws, map[string]string{"data": enc}); err != nil {
			return err
		}
	}
}

type resz struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

func (h *TerminalHandler) auditf(ctx context.Context, actorID *int64, siteID int64, action string, payload any) {
	id := siteID
	_ = h.auditRec.Record(ctx, audit.Event{
		ActorUserID: actorID,
		Action:      action,
		TargetType:  "site",
		TargetID:    &id,
		Payload:     payload,
	})
}

// --- ticket store ---

const ticketTTL = 60 * time.Second

type ticketClaim struct {
	siteID int64
	userID int64
	exp    time.Time
}

type ticketStore struct {
	mu      sync.Mutex
	tickets map[string]ticketClaim
}

func newTicketStore() *ticketStore { return &ticketStore{tickets: make(map[string]ticketClaim)} }

func (t *ticketStore) issue(siteID, userID int64) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)

	t.mu.Lock()
	t.tickets[tok] = ticketClaim{
		siteID: siteID,
		userID: userID,
		exp:    time.Now().Add(ticketTTL),
	}
	t.gcLocked(time.Now())
	t.mu.Unlock()
	return tok, nil
}

func (t *ticketStore) consume(tok string) (ticketClaim, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.tickets[tok]
	if !ok {
		return ticketClaim{}, false
	}
	delete(t.tickets, tok)
	if time.Now().After(c.exp) {
		return ticketClaim{}, false
	}
	return c, true
}

func (t *ticketStore) gcLocked(now time.Time) {
	for tok, c := range t.tickets {
		if now.After(c.exp) {
			delete(t.tickets, tok)
		}
	}
}

// silence unused-import linter if encoding/json gets unused after edits.
var (
	_ = json.Marshal
	_ = strconv.Itoa
)
