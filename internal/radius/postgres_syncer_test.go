package radius

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRadiusExecutor struct {
	sql  string
	args []any
}

func (e *fakeRadiusExecutor) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	e.sql = sql
	e.args = arguments
	return pgconn.CommandTag{}, nil
}

func TestEnsureZywallGuestClassReply(t *testing.T) {
	exec := &fakeRadiusExecutor{}
	syncer := &PostgresSyncer{}

	if err := syncer.ensureZywallGuestClassReply(context.Background(), exec, "cp-test"); err != nil {
		t.Fatalf("ensureZywallGuestClassReply() error = %v", err)
	}

	if !strings.Contains(exec.sql, "INSERT INTO radreply") {
		t.Fatalf("SQL does not insert into radreply: %s", exec.sql)
	}
	if !strings.Contains(exec.sql, "attribute = 'Class'") {
		t.Fatalf("SQL does not check Class attribute: %s", exec.sql)
	}
	if !strings.Contains(exec.sql, "SELECT $1::varchar(64), 'Class', ':='::char(2), $2::varchar(253)") {
		t.Fatalf("SQL does not insert Class := value: %s", exec.sql)
	}
	if !strings.Contains(exec.sql, "value = $2::varchar(253)") {
		t.Fatalf("SQL does not check Class value: %s", exec.sql)
	}
	if !strings.Contains(exec.sql, "SET op = ':='") {
		t.Fatalf("SQL does not normalize existing Class op: %s", exec.sql)
	}
	if !strings.Contains(exec.sql, "WHERE NOT EXISTS") {
		t.Fatalf("SQL is not idempotent: %s", exec.sql)
	}

	if len(exec.args) != 2 {
		t.Fatalf("args len = %d, want 2", len(exec.args))
	}
	if exec.args[0] != "cp-test" {
		t.Fatalf("username arg = %v, want cp-test", exec.args[0])
	}
	if exec.args[1] != zywallGuestClass {
		t.Fatalf("class arg = %v, want %s", exec.args[1], zywallGuestClass)
	}
}
