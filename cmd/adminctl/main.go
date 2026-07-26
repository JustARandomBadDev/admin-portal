package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/JustARandomBadDev/captive-portal-admin/internal/adminauth"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/config"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/database"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/radius"
	"github.com/JustARandomBadDev/captive-portal-admin/internal/tickets"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) != 2 {
		printUsage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "create-admin":
		err = createAdmin()
	case "sync-radius-tickets":
		err = syncRadiusTickets()
	case "cleanup-legacy-radius-class":
		err = cleanupLegacyRadiusClass()
	default:
		printUsage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/adminctl <create-admin|sync-radius-tickets|cleanup-legacy-radius-class>")
}

func createAdmin() error {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, database.Config{URL: cfg.DatabaseURL})
	if err != nil {
		return err
	}
	defer db.Close()

	reader := bufio.NewReader(os.Stdin)
	username, err := readLine(reader, "Username: ")
	if err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username is required")
	}

	password, err := readPassword(reader, "Password: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password is required")
	}

	confirm, err := readPassword(reader, "Confirm password: ")
	if err != nil {
		return err
	}
	if password != confirm {
		return errors.New("password confirmation does not match")
	}

	hash, err := adminauth.HashPassword(password)
	if err != nil {
		return err
	}

	id, err := database.NewUUID()
	if err != nil {
		return err
	}

	_, err = db.Pool().Exec(ctx, `
INSERT INTO admin_users (id, username, password_hash, display_name, is_active)
VALUES ($1, $2, $3, $4, true)
`, id, username, hash, username)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("admin username %q already exists", username)
		}
		return err
	}

	fmt.Printf("Admin created: %s\n", username)
	return nil
}

func syncRadiusTickets() error {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, database.Config{URL: cfg.DatabaseURL})
	if err != nil {
		return err
	}
	defer db.Close()
	if cfg.RadiusDatabaseURL == "" {
		return errors.New("RADIUS_DATABASE_URL is required")
	}
	radiusDB, err := database.Connect(ctx, database.Config{URL: cfg.RadiusDatabaseURL})
	if err != nil {
		return err
	}
	defer radiusDB.Close()

	repository := tickets.NewPostgresRepository(db)
	now := time.Now()
	if _, err := repository.MarkExpired(ctx, now); err != nil {
		return fmt.Errorf("mark expired tickets: %w", err)
	}

	activeTickets, err := repository.ListActive(ctx, now)
	if err != nil {
		return fmt.Errorf("list active tickets: %w", err)
	}

	syncer := radius.NewPostgresSyncer(radiusDB)
	for _, ticket := range activeTickets {
		radiusTicket := radius.Ticket{
			ID:                ticket.ID,
			Username:          ticket.Username,
			CleartextPassword: ticket.CleartextPassword,
			PitchID:           ticket.PitchID,
			ValidFrom:         ticket.ValidFrom,
			ValidUntil:        ticket.ValidUntil,
		}
		if err := syncer.ProvisionTicket(ctx, radiusTicket); err != nil {
			return fmt.Errorf("sync RADIUS ticket %q: %w", ticket.Username, err)
		}
		if err := repository.MarkRadiusSynced(ctx, ticket.ID, time.Now()); err != nil {
			return fmt.Errorf("mark RADIUS synced for ticket %q: %w", ticket.Username, err)
		}
	}

	fmt.Printf("RADIUS tickets synchronized: %d\n", len(activeTickets))
	return nil
}

func cleanupLegacyRadiusClass() error {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if cfg.RadiusDatabaseURL == "" {
		return errors.New("RADIUS_DATABASE_URL is required")
	}
	radiusDB, err := database.Connect(ctx, database.Config{URL: cfg.RadiusDatabaseURL})
	if err != nil {
		return err
	}
	defer radiusDB.Close()

	legacyValue := "WIFI" + "_" + "GUEST"
	var existing int
	if err := radiusDB.Pool().QueryRow(ctx, `
SELECT COUNT(*)
FROM radreply
WHERE attribute = 'Class'
  AND value = $1
`, legacyValue).Scan(&existing); err != nil {
		return fmt.Errorf("count legacy RADIUS Class replies: %w", err)
	}

	tag, err := radiusDB.Pool().Exec(ctx, `
DELETE FROM radreply
WHERE attribute = 'Class'
  AND value = $1
`, legacyValue)
	if err != nil {
		return fmt.Errorf("delete legacy RADIUS Class replies: %w", err)
	}

	fmt.Printf("Legacy RADIUS Class replies found: %d\n", existing)
	fmt.Printf("Legacy RADIUS Class replies deleted: %d\n", tag.RowsAffected())
	return nil
}

func readLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(value, "\r\n"), nil
}

func readPassword(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(syscall.Stdin)) {
		bytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	}

	return readLine(reader, "")
}
