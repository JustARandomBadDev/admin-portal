package tickets

import "time"

// TicketStatus describes the lifecycle state of a WiFi ticket.
type TicketStatus string

const (
	TicketStatusActive  TicketStatus = "active"
	TicketStatusExpired TicketStatus = "expired"
	TicketStatusRevoked TicketStatus = "revoked"
)

// Ticket is the admin-side business record for a temporary WiFi access.
type Ticket struct {
	ID                string
	Username          string
	CleartextPassword string
	PitchID           string
	Status            TicketStatus
	ValidFrom         time.Time
	ValidUntil        time.Time
	CreatedBy         string
	CreatedAt         time.Time
	RevokedAt         *time.Time
	RevokedBy         *string
	RadiusSyncedAt    *time.Time
}

// TicketCreateInput contains the fields required to create a ticket.
type TicketCreateInput struct {
	Username          string
	CleartextPassword string
	PitchID           string
	PitchCode         string
	ValidFrom         time.Time
	ValidUntil        time.Time
	CreatedBy         string
}

// TicketRevokeInput contains the fields required to revoke an active ticket.
type TicketRevokeInput struct {
	ID        string
	RevokedBy string
	RevokedAt time.Time
}

// TicketListFilters contains the optional filters used by the admin ticket list.
type TicketListFilters struct {
	Search       string
	Status       TicketStatus
	Duration     time.Duration
	CreatedSince *time.Time
}
