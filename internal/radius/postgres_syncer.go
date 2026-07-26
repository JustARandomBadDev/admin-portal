package radius

import (
	"context"
	"fmt"
	"time"

	"github.com/JustARandomBadDev/captive-portal-admin/internal/database"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	radiusAttributePassword        = "Cleartext-Password"
	radiusAttributeExpiration      = "Expiration"
	radiusAttributeSimultaneousUse = "Simultaneous-Use"
	simultaneousUseLimit           = "4"
)

type PostgresSyncer struct {
	pool *pgxpool.Pool
}

type radiusExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func NewPostgresSyncer(db *database.Handle) *PostgresSyncer {
	return &PostgresSyncer{pool: db.Pool()}
}

func (s *PostgresSyncer) ProvisionTicket(ctx context.Context, ticket Ticket) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `
INSERT INTO radius_users (id, username, cleartext_password, is_active, expires_at)
VALUES ($1, $2, $3, true, $4)
ON CONFLICT (username) DO UPDATE
SET cleartext_password = EXCLUDED.cleartext_password,
    is_active = true,
    expires_at = EXCLUDED.expires_at,
    updated_at = now()
`, ticket.ID, ticket.Username, ticket.CleartextPassword, ticket.ValidUntil); err != nil {
		return fmt.Errorf("upsert radius user %q: %w", ticket.Username, err)
	}

	if err := s.syncRadcheckAccessPolicy(ctx, tx, ticket); err != nil {
		return fmt.Errorf("sync radcheck access policy for ticket %q: %w", ticket.Username, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit radius ticket provisioning for %q: %w", ticket.Username, err)
	}

	return nil
}

func (s *PostgresSyncer) syncRadcheckAccessPolicy(ctx context.Context, exec radiusExecutor, ticket Ticket) error {
	if _, err := exec.Exec(ctx, `
DELETE FROM radcheck
WHERE username = $1
  AND attribute IN ('Cleartext-Password', 'Expiration', 'Simultaneous-Use')
`, ticket.Username); err != nil {
		return fmt.Errorf("delete managed radcheck attributes: %w", err)
	}

	if _, err := exec.Exec(ctx, `
INSERT INTO radcheck (username, attribute, op, value)
VALUES ($1, 'Cleartext-Password', ':=', $2)
`, ticket.Username, ticket.CleartextPassword); err != nil {
		return fmt.Errorf("insert Cleartext-Password check item: %w", err)
	}

	if _, err := exec.Exec(ctx, `
INSERT INTO radcheck (username, attribute, op, value)
VALUES ($1, 'Expiration', ':=', $2)
`, ticket.Username, FormatRADIUSExpiration(ticket.ValidUntil)); err != nil {
		return fmt.Errorf("insert Expiration check item: %w", err)
	}

	if _, err := exec.Exec(ctx, `
INSERT INTO radcheck (username, attribute, op, value)
VALUES ($1, 'Simultaneous-Use', ':=', $2)
`, ticket.Username, simultaneousUseLimit); err != nil {
		return fmt.Errorf("insert Simultaneous-Use check item: %w", err)
	}

	return nil
}

func (s *PostgresSyncer) RevokeTicket(ctx context.Context, ticket Ticket) error {
	return s.removeCredentials(ctx, ticket.Username)
}

func (s *PostgresSyncer) DeleteExpiredTicket(ctx context.Context, ticket Ticket) error {
	return s.removeCredentials(ctx, ticket.Username)
}

func (s *PostgresSyncer) removeCredentials(ctx context.Context, username string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `
DELETE FROM radcheck
WHERE username = $1
`, username); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
DELETE FROM radreply
WHERE username = $1
`, username); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
DELETE FROM radusergroup
WHERE username = $1
`, username); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
UPDATE radius_users
SET cleartext_password = NULL,
    is_active = false,
    updated_at = now()
WHERE username = $1
`, username); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// FormatRADIUSExpiration formats a ticket validity instant for FreeRADIUS 3.x.
// The admin UI computes validity in the camping's local time (Europe/Paris);
// the stored time.Time represents that exact instant. UTC output avoids DST and
// container timezone ambiguity while preserving the same instant.
func FormatRADIUSExpiration(value time.Time) string {
	return value.UTC().Format("02 Jan 2006 15:04:05 UTC")
}
