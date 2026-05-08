//go:build integration

package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		// Let individual tests skip; don't exit here so go test reports correctly.
		os.Exit(m.Run())
	}

	if err := RunMigrations(dbURL); err != nil {
		panic("TestMain: migrations failed: " + err.Error())
	}

	var err error
	testPool, err = pgxpool.New(context.Background(), dbURL)
	if err != nil {
		panic("TestMain: pool open failed: " + err.Error())
	}
	defer testPool.Close()

	os.Exit(m.Run())
}

// truncateAll wipes all tables and re-seeds settings defaults.
// Use at the start of each test that needs a clean slate.
func truncateAll(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("TEST_DATABASE_URL not set")
	}
	_, err := testPool.Exec(context.Background(), `
		TRUNCATE users, absence, audit_log RESTART IDENTITY CASCADE;
		DELETE FROM settings;
		INSERT INTO settings (key, value, updated_at, updated_by) VALUES
		  ('min_present',   '8',     now(), 'system'),
		  ('team_size',     '15',    now(), 'system'),
		  ('weekend_counts','false', now(), 'system');
	`)
	if err != nil {
		t.Fatalf("truncateAll: %v", err)
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	truncateAll(t)
	return &Store{pool: testPool}
}
