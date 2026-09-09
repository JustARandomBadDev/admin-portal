package database

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/JustARandomBadDev/captive-portal-admin/migrations"
)

func TestUnreadableMigration(t *testing.T) {
	files := fstest.MapFS{"001_directory.sql": {Mode: fs.ModeDir}}
	if _, err := discoverMigrations(files); err == nil {
		t.Fatal("expected migration read error")
	}
}

func TestEmbeddedMigrations(t *testing.T) {
	items, err := discoverMigrations(migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d migrations", len(items))
	}
	for i, version := range []string{"001", "002"} {
		if items[i].version != version || items[i].sql == "" {
			t.Fatalf("unexpected migration: %+v", items[i])
		}
	}
}

func TestMigrationNumericOrder(t *testing.T) {
	items, err := discoverMigrations(fstest.MapFS{"10_later.sql": {Data: []byte("SELECT 1;")}, "2_first.sql": {Data: []byte("SELECT 2;")}})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].version != "2" || items[1].version != "10" {
		t.Fatalf("wrong order: %+v", items)
	}
}

func TestInvalidMigrations(t *testing.T) {
	for _, names := range [][]string{{"schema.sql"}, {"001.sql"}, {"001_.sql"}, {"18446744073709551616_overflow.sql"}, {"001_one.sql", "001_two.sql"}, {"1_one.sql", "001_two.sql"}} {
		t.Run(names[0], func(t *testing.T) {
			files := fstest.MapFS{}
			for _, name := range names {
				files[name] = &fstest.MapFile{Data: []byte("SELECT 1;")}
			}
			if _, err := discoverMigrations(files); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
