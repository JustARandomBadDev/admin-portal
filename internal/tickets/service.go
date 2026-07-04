package tickets

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/JustARandomBadDev/captive-portal-admin/internal/radius"
)

var (
	ErrInvalidTicketStatus = errors.New("invalid ticket status")
	ErrInvalidTicketDates  = errors.New("valid_until must be after valid_from")
	ErrPitchRequired       = errors.New("pitch_id is required")
	ErrPitchCodeRequired   = errors.New("pitch code is required to generate username")
)

const generatedPasswordLength = 7

var pitchNumberExpression = regexp.MustCompile(`\d+`)

type Service struct {
	repository Repository
	radiusSync radius.Syncer
	now        func() time.Time
}

func NewService(repository Repository, syncer ...radius.Syncer) *Service {
	radiusSync := radius.Syncer(radius.NoopSyncer{})
	if len(syncer) > 0 && syncer[0] != nil {
		radiusSync = syncer[0]
	}

	return &Service{
		repository: repository,
		radiusSync: radiusSync,
		now:        time.Now,
	}
}

func (s *Service) Create(ctx context.Context, input TicketCreateInput) (Ticket, error) {
	input.PitchID = strings.TrimSpace(input.PitchID)
	input.PitchCode = strings.TrimSpace(input.PitchCode)
	input.Username = strings.TrimSpace(input.Username)
	input.CleartextPassword = strings.TrimSpace(input.CleartextPassword)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)

	if input.PitchID == "" {
		return Ticket{}, ErrPitchRequired
	}
	if !input.ValidUntil.After(input.ValidFrom) {
		return Ticket{}, ErrInvalidTicketDates
	}

	generateUsername := input.Username == ""
	if generateUsername && pitchNumber(input.PitchCode) == "" {
		return Ticket{}, ErrPitchCodeRequired
	}
	if input.CleartextPassword == "" {
		password, err := GeneratePassword(generatedPasswordLength)
		if err != nil {
			return Ticket{}, err
		}
		input.CleartextPassword = password
	}

	for attempt := 0; attempt < 100; attempt++ {
		if generateUsername {
			username, err := GenerateUsername(input.PitchCode)
			if err != nil {
				return Ticket{}, err
			}
			input.Username = username
		}

		ticket, err := s.repository.Create(ctx, input)
		if err == nil {
			if err := s.radiusSync.ProvisionTicket(ctx, radiusTicket(ticket)); err != nil {
				slog.WarnContext(ctx, "radius provision failed after ticket creation", "ticket_id", ticket.ID, "username", ticket.Username, "error", err)
			} else {
				s.markRadiusSynced(ctx, ticket)
			}
			return ticket, nil
		}
		if !generateUsername || !errors.Is(err, ErrDuplicateUsername) {
			return Ticket{}, err
		}
	}

	return Ticket{}, ErrDuplicateUsername
}

func (s *Service) ListAll(ctx context.Context) ([]Ticket, error) {
	if _, err := s.repository.MarkExpired(ctx, s.now()); err != nil {
		return nil, err
	}

	tickets, err := s.repository.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	for _, ticket := range tickets {
		if ticket.Status != TicketStatusExpired || ticket.RadiusSyncedAt != nil {
			continue
		}
		if err := s.radiusSync.DeleteExpiredTicket(ctx, radiusTicket(ticket)); err != nil {
			slog.WarnContext(ctx, "radius expired ticket cleanup failed", "ticket_id", ticket.ID, "username", ticket.Username, "error", err)
			continue
		}
		s.markRadiusSynced(ctx, ticket)
	}

	return tickets, nil
}

func (s *Service) ListFiltered(ctx context.Context, filters TicketListFilters) ([]Ticket, error) {
	if _, err := s.repository.MarkExpired(ctx, s.now()); err != nil {
		return nil, err
	}

	tickets, err := s.repository.ListFiltered(ctx, filters)
	if err != nil {
		return nil, err
	}

	for _, ticket := range tickets {
		if ticket.Status != TicketStatusExpired || ticket.RadiusSyncedAt != nil {
			continue
		}
		if err := s.radiusSync.DeleteExpiredTicket(ctx, radiusTicket(ticket)); err != nil {
			slog.WarnContext(ctx, "radius expired ticket cleanup failed", "ticket_id", ticket.ID, "username", ticket.Username, "error", err)
			continue
		}
		s.markRadiusSynced(ctx, ticket)
	}

	return tickets, nil
}

func (s *Service) Revoke(ctx context.Context, input TicketRevokeInput) (Ticket, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.RevokedBy = strings.TrimSpace(input.RevokedBy)
	if input.RevokedAt.IsZero() {
		input.RevokedAt = s.now()
	}

	ticket, err := s.repository.Revoke(ctx, input)
	if err != nil {
		return Ticket{}, err
	}
	if err := s.radiusSync.RevokeTicket(ctx, radiusTicket(ticket)); err != nil {
		slog.WarnContext(ctx, "radius revoke failed after ticket revocation", "ticket_id", ticket.ID, "username", ticket.Username, "error", err)
	} else {
		s.markRadiusSynced(ctx, ticket)
	}

	return ticket, nil
}

func (s *Service) Repository() Repository {
	return s.repository
}

func (s *Service) markRadiusSynced(ctx context.Context, ticket Ticket) {
	if err := s.repository.MarkRadiusSynced(ctx, ticket.ID, s.now()); err != nil {
		slog.WarnContext(ctx, "mark radius sync failed", "ticket_id", ticket.ID, "username", ticket.Username, "error", err)
	}
}

func radiusTicket(ticket Ticket) radius.Ticket {
	return radius.Ticket{
		ID:                ticket.ID,
		Username:          ticket.Username,
		CleartextPassword: ticket.CleartextPassword,
		PitchID:           ticket.PitchID,
		ValidFrom:         ticket.ValidFrom,
		ValidUntil:        ticket.ValidUntil,
	}
}

func IsValidStatus(status TicketStatus) bool {
	switch status {
	case TicketStatusActive, TicketStatusExpired, TicketStatusRevoked:
		return true
	default:
		return false
	}
}

func DurationFromDates(validFrom, validUntil time.Time) (time.Duration, error) {
	if !validUntil.After(validFrom) {
		return 0, ErrInvalidTicketDates
	}
	return validUntil.Sub(validFrom), nil
}

func GenerateUsername(pitchCode string) (string, error) {
	number := pitchNumber(pitchCode)
	if number == "" {
		return "", ErrPitchCodeRequired
	}

	suffix, err := randomString("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", 4)
	if err != nil {
		return "", err
	}

	return "XXX" + number + "-" + suffix, nil
}

func GeneratePassword(length int) (string, error) {
	return randomString("ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789", length)
}

func randomString(alphabet string, length int) (string, error) {
	result := make([]byte, length)
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}

	for i, value := range random {
		result[i] = alphabet[int(value)%len(alphabet)]
	}

	return string(result), nil
}

func pitchNumber(code string) string {
	return strings.Join(pitchNumberExpression.FindAllString(code, -1), "")
}
