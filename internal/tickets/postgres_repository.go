package tickets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JustARandomBadDev/captive-portal-admin/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTicketNotFound      = errors.New("ticket not found")
	ErrDuplicateUsername   = errors.New("ticket username already exists")
	ErrInvalidTicketChange = errors.New("invalid ticket change")
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(db *database.Handle) *PostgresRepository {
	return &PostgresRepository{pool: db.Pool()}
}

func (r *PostgresRepository) Create(ctx context.Context, input TicketCreateInput) (Ticket, error) {
	id, err := database.NewUUID()
	if err != nil {
		return Ticket{}, err
	}

	row := r.pool.QueryRow(ctx, `
INSERT INTO wifi_tickets (
    id, username, cleartext_password, pitch_id, status,
    valid_from, valid_until, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING
    id::text, username, cleartext_password, pitch_id::text, status,
    valid_from, valid_until, created_by::text, created_at,
    revoked_at, revoked_by::text, radius_synced_at
`,
		id,
		input.Username,
		input.CleartextPassword,
		input.PitchID,
		TicketStatusActive,
		input.ValidFrom,
		input.ValidUntil,
		nullableString(input.CreatedBy),
	)

	ticket, err := scanTicket(row)
	if err != nil {
		return Ticket{}, mapPostgresError(err)
	}

	return ticket, nil
}

func (r *PostgresRepository) DeleteByID(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `
DELETE FROM wifi_tickets
WHERE id = $1
`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTicketNotFound
	}

	return nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, id string) (Ticket, error) {
	row := r.pool.QueryRow(ctx, ticketSelectSQL()+` WHERE id = $1`, id)

	ticket, err := scanTicket(row)
	if err != nil {
		return Ticket{}, mapPostgresError(err)
	}

	return ticket, nil
}

func (r *PostgresRepository) ListActive(ctx context.Context, now time.Time) ([]Ticket, error) {
	rows, err := r.pool.Query(ctx, ticketSelectSQL()+`
WHERE status = $1
  AND valid_from <= $2
  AND valid_until > $2
ORDER BY valid_until ASC, created_at ASC
`, TicketStatusActive, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTickets(rows)
}

func (r *PostgresRepository) ListAll(ctx context.Context) ([]Ticket, error) {
	rows, err := r.pool.Query(ctx, ticketSelectSQL()+`
ORDER BY created_at DESC, username ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTickets(rows)
}

func (r *PostgresRepository) ListFiltered(ctx context.Context, filters TicketListFilters) ([]Ticket, error) {
	query := `
SELECT
    wt.id::text, wt.username, wt.cleartext_password, wt.pitch_id::text, wt.status,
    wt.valid_from, wt.valid_until, wt.created_by::text, wt.created_at,
    wt.revoked_at, wt.revoked_by::text, wt.radius_synced_at
FROM wifi_tickets wt
JOIN pitches p ON p.id = wt.pitch_id
`
	where := make([]string, 0, 4)
	args := make([]any, 0, 4)

	if filters.Search != "" {
		args = append(args, "%"+filters.Search+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		where = append(where, "(wt.username ILIKE "+placeholder+" OR p.code ILIKE "+placeholder+" OR p.label ILIKE "+placeholder+")")
	}
	if filters.Status != "" {
		args = append(args, filters.Status)
		where = append(where, fmt.Sprintf("wt.status = $%d", len(args)))
	}
	if filters.Duration > 0 {
		args = append(args, int(filters.Duration.Seconds()))
		where = append(where, fmt.Sprintf("wt.valid_until = wt.valid_from + make_interval(secs => $%d)", len(args)))
	}
	if filters.CreatedSince != nil {
		args = append(args, *filters.CreatedSince)
		where = append(where, fmt.Sprintf("wt.created_at >= $%d", len(args)))
	}

	if len(where) > 0 {
		query += "WHERE " + strings.Join(where, "\n  AND ") + "\n"
	}
	query += "ORDER BY wt.created_at DESC, wt.username ASC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTickets(rows)
}

func (r *PostgresRepository) Revoke(ctx context.Context, input TicketRevokeInput) (Ticket, error) {
	row := r.pool.QueryRow(ctx, `
UPDATE wifi_tickets
SET status = $2,
    revoked_at = $3,
    revoked_by = $4,
    radius_synced_at = NULL
WHERE id = $1
RETURNING
    id::text, username, cleartext_password, pitch_id::text, status,
    valid_from, valid_until, created_by::text, created_at,
    revoked_at, revoked_by::text, radius_synced_at
`,
		input.ID,
		TicketStatusRevoked,
		input.RevokedAt,
		nullableString(input.RevokedBy),
	)

	ticket, err := scanTicket(row)
	if err != nil {
		return Ticket{}, mapPostgresError(err)
	}

	return ticket, nil
}

func (r *PostgresRepository) MarkExpired(ctx context.Context, now time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx, `
UPDATE wifi_tickets
SET status = $1,
    radius_synced_at = NULL
WHERE status = $2
  AND valid_until <= $3
`, TicketStatusExpired, TicketStatusActive, now)
	if err != nil {
		return 0, err
	}

	return int(tag.RowsAffected()), nil
}

func (r *PostgresRepository) MarkRadiusSynced(ctx context.Context, id string, syncedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE wifi_tickets
SET radius_synced_at = $2
WHERE id = $1
`, id, syncedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTicketNotFound
	}

	return nil
}

func (r *PostgresRepository) DeleteOldExpired(ctx context.Context, before time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx, `
DELETE FROM wifi_tickets
WHERE status = $1
  AND valid_until < $2
`, TicketStatusExpired, before)
	if err != nil {
		return 0, err
	}

	return int(tag.RowsAffected()), nil
}

func ticketSelectSQL() string {
	return `
SELECT
    id::text, username, cleartext_password, pitch_id::text, status,
    valid_from, valid_until, created_by::text, created_at,
    revoked_at, revoked_by::text, radius_synced_at
FROM wifi_tickets
`
}

func scanTickets(rows pgx.Rows) ([]Ticket, error) {
	tickets := make([]Ticket, 0)
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			return nil, mapPostgresError(err)
		}
		tickets = append(tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tickets, nil
}

type ticketScanner interface {
	Scan(dest ...any) error
}

func scanTicket(scanner ticketScanner) (Ticket, error) {
	var ticket Ticket
	var status string
	var createdBy sql.NullString
	var revokedAt sql.NullTime
	var revokedBy sql.NullString
	var radiusSyncedAt sql.NullTime

	if err := scanner.Scan(
		&ticket.ID,
		&ticket.Username,
		&ticket.CleartextPassword,
		&ticket.PitchID,
		&status,
		&ticket.ValidFrom,
		&ticket.ValidUntil,
		&createdBy,
		&ticket.CreatedAt,
		&revokedAt,
		&revokedBy,
		&radiusSyncedAt,
	); err != nil {
		return Ticket{}, err
	}

	ticket.Status = TicketStatus(status)
	if createdBy.Valid {
		ticket.CreatedBy = createdBy.String
	}
	if revokedAt.Valid {
		ticket.RevokedAt = &revokedAt.Time
	}
	if revokedBy.Valid {
		ticket.RevokedBy = &revokedBy.String
	}
	if radiusSyncedAt.Valid {
		ticket.RadiusSyncedAt = &radiusSyncedAt.Time
	}

	return ticket, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func mapPostgresError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTicketNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" && pgErr.ConstraintName == "wifi_tickets_username_key" {
			return ErrDuplicateUsername
		}
		if pgErr.Code == "23514" {
			return ErrInvalidTicketChange
		}
	}

	return err
}
