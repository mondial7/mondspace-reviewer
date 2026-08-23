package postgres_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/postgres"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// newStore opens a store against a throwaway schema, skipping unless a DSN is
// configured so the default `go test ./...` stays hermetic and offline.
func newStore(t *testing.T) (*postgres.Store, string) {
	t.Helper()
	dsn := os.Getenv("MSR_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set MSR_POSTGRES_DSN to run the Postgres store tests")
	}
	// Unquoted PostgreSQL identifiers fold to lower case, so schema names are
	// lower case by construction.
	schema := "msr_test_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))

	ctx := context.Background()
	s, err := postgres.Open(ctx, dsn, schema)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		pool, err := pgxpool.New(context.Background(), dsn)
		if err == nil {
			_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
			pool.Close()
		}
		s.Close()
	})
	return s, schema
}

func TestNeverCreatesObjectsInPublicSchema(t *testing.T) {
	s, schema := newStore(t)

	// Everything the store writes must land in its own schema.
	if err := s.AppendEvent(domain.Event{ID: "e1", SessionID: "s", Kind: domain.KindEdit}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), os.Getenv("MSR_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var inSchema int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = $1`, schema).Scan(&inSchema); err != nil {
		t.Fatal(err)
	}
	if inSchema == 0 {
		t.Errorf("expected tables in schema %q, found none", schema)
	}

	var inPublic int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name IN ('events','units','notes')`).Scan(&inPublic); err != nil {
		t.Fatal(err)
	}
	if inPublic != 0 {
		t.Errorf("store created %d of its tables in the public schema; it must not", inPublic)
	}
}

func TestRoundTripsSessionState(t *testing.T) {
	s, _ := newStore(t)

	ts := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	events := []domain.Event{
		{ID: "e1", SessionID: "s", TS: ts, Kind: domain.KindPrompt, StatedIntent: "add auth"},
		{ID: "e2", SessionID: "s", TS: ts.Add(time.Second), Kind: domain.KindEdit, Files: []string{"a.go"}, Tool: "Edit"},
	}
	for _, e := range events {
		if err := s.AppendEvent(e); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	unit := domain.Unit{ID: "s-f001", SessionID: "s", Files: []string{"a.go"}, Sealed: true,
		Flags: []domain.Flag{domain.FlagNoTest}, Headline: domain.Headline{Text: "edited a.go", WhySrc: domain.WhyInferred}}
	if err := s.AppendUnit(unit); err != nil {
		t.Fatalf("AppendUnit: %v", err)
	}
	note := domain.Note{ID: "n1", SessionID: "s", UnitID: "s-f001", Kind: domain.NoteObjection, Text: "wrong layer", TS: ts}
	if err := s.AppendNote(note); err != nil {
		t.Fatalf("AppendNote: %v", err)
	}

	sess, err := s.Load("s")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if sess.ID != "s" || sess.Prompt != "add auth" {
		t.Errorf("session = %+v, want id s and the prompt", sess)
	}
	if len(sess.Events) != 2 || sess.Events[0].ID != "e1" {
		t.Errorf("events = %+v, want 2 in order", sess.Events)
	}
	if len(sess.Units) != 1 || sess.Units[0].Headline.Text != "edited a.go" {
		t.Errorf("units = %+v", sess.Units)
	}
	if len(sess.Units[0].Flags) != 1 || sess.Units[0].Flags[0] != domain.FlagNoTest {
		t.Errorf("unit flags not round-tripped: %+v", sess.Units[0].Flags)
	}
	if len(sess.Notes) != 1 || sess.Notes[0].Text != "wrong layer" {
		t.Errorf("notes = %+v", sess.Notes)
	}
}

func TestAppendIsIdempotentOnID(t *testing.T) {
	s, _ := newStore(t)

	e := domain.Event{ID: "e1", SessionID: "s", Kind: domain.KindEdit}
	for i := 0; i < 2; i++ {
		if err := s.AppendEvent(e); err != nil {
			t.Fatalf("AppendEvent %d: %v", i, err)
		}
	}

	sess, err := s.Load("s")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Events) != 1 {
		t.Errorf("re-appending the same id produced %d events, want 1", len(sess.Events))
	}
}

func TestSchemaNameIsValidated(t *testing.T) {
	if os.Getenv("MSR_POSTGRES_DSN") == "" {
		t.Skip("set MSR_POSTGRES_DSN to run the Postgres store tests")
	}
	// `public` is refused outright; anything that is not a plain identifier is
	// rejected rather than escaped, because the name is interpolated into DDL.
	for _, bad := range []string{"public", "has space", "drop;table", `"quoted"`, "Mixed", "x-y"} {
		if _, err := postgres.Open(context.Background(), os.Getenv("MSR_POSTGRES_DSN"), bad); err == nil {
			t.Errorf("schema %q should be rejected", bad)
		}
	}
}
