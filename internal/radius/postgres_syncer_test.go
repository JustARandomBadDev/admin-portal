package radius

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRadiusExecutor struct {
	calls []fakeRadiusExecCall
	errAt int
	err   error
}

type fakeRadiusExecCall struct {
	sql  string
	args []any
}

func (e *fakeRadiusExecutor) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	e.calls = append(e.calls, fakeRadiusExecCall{sql: sql, args: arguments})
	if e.err != nil && len(e.calls) == e.errAt {
		return pgconn.CommandTag{}, e.err
	}
	return pgconn.CommandTag{}, nil
}

func TestFormatRADIUSExpirationUsesUTC(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("load Europe/Paris: %v", err)
	}

	tests := []struct {
		name  string
		value time.Time
		want  string
	}{
		{
			name:  "winter paris noon",
			value: time.Date(2026, 1, 15, 12, 34, 56, 0, paris),
			want:  "15 Jan 2026 11:34:56 UTC",
		},
		{
			name:  "summer paris end of day",
			value: time.Date(2026, 7, 26, 23, 59, 59, 0, paris),
			want:  "26 Jul 2026 21:59:59 UTC",
		},
		{
			name:  "daylight saving start instant",
			value: time.Date(2026, 3, 29, 3, 30, 0, 0, paris),
			want:  "29 Mar 2026 01:30:00 UTC",
		},
		{
			name:  "daylight saving end instant",
			value: time.Date(2026, 10, 25, 2, 30, 0, 0, paris),
			want:  "25 Oct 2026 01:30:00 UTC",
		},
		{
			name:  "end of selected local day",
			value: time.Date(2026, 8, 15, 23, 59, 59, 0, paris),
			want:  "15 Aug 2026 21:59:59 UTC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatRADIUSExpiration(tt.value); got != tt.want {
				t.Fatalf("FormatRADIUSExpiration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSyncRadcheckAccessPolicyReplacesManagedAttributes(t *testing.T) {
	exec := &fakeRadiusExecutor{}
	syncer := &PostgresSyncer{}
	validUntil := time.Date(2026, 7, 26, 21, 59, 59, 0, time.UTC)

	err := syncer.syncRadcheckAccessPolicy(context.Background(), exec, Ticket{
		Username:          "cp-test",
		CleartextPassword: "secret",
		ValidUntil:        validUntil,
	})
	if err != nil {
		t.Fatalf("syncRadcheckAccessPolicy() error = %v", err)
	}
	if len(exec.calls) != 4 {
		t.Fatalf("exec calls = %d, want 4", len(exec.calls))
	}

	deleteSQL := exec.calls[0].sql
	for _, attribute := range []string{radiusAttributePassword, radiusAttributeExpiration, radiusAttributeSimultaneousUse} {
		if !strings.Contains(deleteSQL, "'"+attribute+"'") {
			t.Fatalf("delete SQL does not target %s: %s", attribute, deleteSQL)
		}
	}

	inserted := map[string]any{}
	for _, call := range exec.calls[1:] {
		if strings.Contains(strings.ToLower(call.sql), "radreply") {
			t.Fatalf("managed access policy must not write radreply: %s", call.sql)
		}
		switch {
		case strings.Contains(call.sql, "'Cleartext-Password'"):
			inserted[radiusAttributePassword] = call.args[1]
		case strings.Contains(call.sql, "'Expiration'"):
			inserted[radiusAttributeExpiration] = call.args[1]
		case strings.Contains(call.sql, "'Simultaneous-Use'"):
			inserted[radiusAttributeSimultaneousUse] = call.args[1]
		}
	}

	if inserted[radiusAttributePassword] != "secret" {
		t.Fatalf("Cleartext-Password value = %v, want secret", inserted[radiusAttributePassword])
	}
	if inserted[radiusAttributeExpiration] != "26 Jul 2026 21:59:59 UTC" {
		t.Fatalf("Expiration value = %v", inserted[radiusAttributeExpiration])
	}
	if inserted[radiusAttributeSimultaneousUse] != simultaneousUseLimit {
		t.Fatalf("Simultaneous-Use value = %v, want %s", inserted[radiusAttributeSimultaneousUse], simultaneousUseLimit)
	}
}

func TestSyncRadcheckAccessPolicyReturnsContextualError(t *testing.T) {
	wantErr := errors.New("database unavailable")
	exec := &fakeRadiusExecutor{errAt: 3, err: wantErr}
	syncer := &PostgresSyncer{}

	err := syncer.syncRadcheckAccessPolicy(context.Background(), exec, Ticket{
		Username:          "cp-test",
		CleartextPassword: "secret",
		ValidUntil:        time.Date(2026, 7, 26, 21, 59, 59, 0, time.UTC),
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("syncRadcheckAccessPolicy() error = %v, want wrapped %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "insert Expiration check item") {
		t.Fatalf("error lacks context: %v", err)
	}
}
