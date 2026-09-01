// Package postgres stores review state in PostgreSQL for the web app (ADR 0007).
//
// Every object lives in a dedicated schema — never `public` — so the tool can
// share a database with other applications without colliding with their tables.
// All statements are schema-qualified rather than relying on the search path.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// DefaultSchema is where review state lives unless configured otherwise.
const DefaultSchema = "mondspace_reviewer"

// safeSchema matches an unquoted PostgreSQL identifier. Schema names are
// interpolated into DDL, so they are validated rather than escaped.
var safeSchema = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// Store is a port.Store backed by PostgreSQL.
type Store struct {
	pool   *pgxpool.Pool
	schema string
}

// Open connects, validates the schema name, and creates the schema and tables if
// they do not exist. Passing "" uses DefaultSchema; "public" is rejected.
func Open(ctx context.Context, dsn, schema string) (*Store, error) {
	if schema == "" {
		schema = DefaultSchema
	}
	if schema == "public" {
		return nil, fmt.Errorf("postgres: refusing to use the public schema; pick a dedicated one")
	}
	if !safeSchema.MatchString(schema) {
		return nil, fmt.Errorf("postgres: invalid schema name %q", schema)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{pool: pool, schema: schema}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() { s.pool.Close() }

// migrate creates the schema and tables idempotently.
func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE SCHEMA IF NOT EXISTS ` + s.schema,
		`CREATE TABLE IF NOT EXISTS ` + s.table("events") + ` (
			id          text PRIMARY KEY,
			session_id  text NOT NULL,
			ts          timestamptz NOT NULL,
			seq         bigserial NOT NULL,
			payload     jsonb NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("units") + ` (
			id          text PRIMARY KEY,
			session_id  text NOT NULL,
			seq         bigserial NOT NULL,
			payload     jsonb NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("notes") + ` (
			id          text PRIMARY KEY,
			session_id  text NOT NULL,
			unit_id     text NOT NULL,
			ts          timestamptz NOT NULL,
			seq         bigserial NOT NULL,
			payload     jsonb NOT NULL
		)`,
		// A session has one current story, not a history of them, so the session
		// id is the key and re-narrating replaces the row.
		`CREATE TABLE IF NOT EXISTS ` + s.table("narratives") + ` (
			session_id  text PRIMARY KEY,
			updated_at  timestamptz NOT NULL DEFAULT now(),
			payload     jsonb NOT NULL
		)`,
		// A target has one current verdict, and re-signing replaces it
		// (ADR 0021).
		`CREATE TABLE IF NOT EXISTS ` + s.table("signoffs") + ` (
			target_id   text PRIMARY KEY,
			updated_at  timestamptz NOT NULL DEFAULT now(),
			payload     jsonb NOT NULL
		)`,
		// One row per (target, audit, diff), because two audits run
		// independently and must not overwrite each other (ADR 0024), and
		// because coming back to a review that has not moved should not cost a
		// model call for an answer already on disk (ADR 0037).
		`CREATE TABLE IF NOT EXISTS ` + s.table("analyses") + ` (
			target_id   text NOT NULL,
			kind        text NOT NULL,
			print       text NOT NULL DEFAULT '',
			updated_at  timestamptz NOT NULL DEFAULT now(),
			payload     jsonb NOT NULL,
			PRIMARY KEY (target_id, kind, print)
		)`,
		// And the same for a table created before the diff was part of the key.
		// Dropping and re-adding the constraint is the idempotent spelling; the
		// table holds one row per click on an audit button, so rebuilding its
		// index at start-up is not a cost worth writing dynamic SQL to avoid.
		`ALTER TABLE ` + s.table("analyses") + ` ADD COLUMN IF NOT EXISTS print text NOT NULL DEFAULT ''`,
		`ALTER TABLE ` + s.table("analyses") + ` DROP CONSTRAINT IF EXISTS analyses_pkey`,
		`ALTER TABLE ` + s.table("analyses") + ` ADD CONSTRAINT analyses_pkey PRIMARY KEY (target_id, kind, print)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("exchanges") + ` (
			id          text PRIMARY KEY,
			session_id  text NOT NULL,
			ts          timestamptz NOT NULL,
			seq         bigserial NOT NULL,
			payload     jsonb NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS exchanges_session_idx ON ` + s.table("exchanges") + ` (session_id, seq)`,
		`CREATE INDEX IF NOT EXISTS events_session_idx ON ` + s.table("events") + ` (session_id, seq)`,
		`CREATE INDEX IF NOT EXISTS units_session_idx ON ` + s.table("units") + ` (session_id, seq)`,
		`CREATE INDEX IF NOT EXISTS notes_session_idx ON ` + s.table("notes") + ` (session_id, seq)`,
	}
	for _, q := range stmts {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("postgres: migrate: %w", err)
		}
	}
	return nil
}

func (s *Store) table(name string) string { return s.schema + "." + name }

func (s *Store) AppendEvent(e domain.Event) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(),
		`INSERT INTO `+s.table("events")+` (id, session_id, ts, payload)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		e.ID, e.SessionID, nonZeroTime(e.TS), payload)
	return err
}

func (s *Store) AppendUnit(u domain.Unit) error {
	payload, err := json.Marshal(u)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(),
		`INSERT INTO `+s.table("units")+` (id, session_id, payload)
		 VALUES ($1, $2, $3) ON CONFLICT (id) DO UPDATE SET payload = EXCLUDED.payload`,
		u.ID, u.SessionID, payload)
	return err
}

func (s *Store) AppendNote(n domain.Note) error {
	payload, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(),
		`INSERT INTO `+s.table("notes")+` (id, session_id, unit_id, ts, payload)
		 VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO NOTHING`,
		n.ID, n.SessionID, n.UnitID, nonZeroTime(n.TS), payload)
	return err
}

// AppendExchange records one question and its answer, so the review
// conversation outlives the process that had it.
func (s *Store) AppendExchange(e domain.Exchange) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(),
		`INSERT INTO `+s.table("exchanges")+` (id, session_id, ts, payload)
		 VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		e.ID, e.SessionID, nonZeroTime(e.TS), payload)
	return err
}

// SaveNarrative stores a session's story so it survives a restart. Narration
// costs several model calls; without this every launch pays for them again.
func (s *Store) SaveNarrative(n domain.Narrative) error {
	payload, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(),
		`INSERT INTO `+s.table("narratives")+` (session_id, payload)
		 VALUES ($1, $2)
		 ON CONFLICT (session_id) DO UPDATE SET payload = EXCLUDED.payload, updated_at = now()`,
		n.SessionID, payload)
	return err
}

// LoadNarrative returns the stored story, or a zero Narrative when the session
// has never been narrated. That is an ordinary state, not a failure: the caller
// narrates it.
func (s *Store) LoadNarrative(sessionID string) (domain.Narrative, error) {
	var payload []byte
	err := s.pool.QueryRow(context.Background(),
		`SELECT payload FROM `+s.table("narratives")+` WHERE session_id = $1`,
		sessionID).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Narrative{}, nil
	}
	if err != nil {
		return domain.Narrative{}, err
	}
	var n domain.Narrative
	if err := json.Unmarshal(payload, &n); err != nil {
		// A corrupt story is worth re-narrating, not worth failing over.
		return domain.Narrative{}, nil
	}
	return n, nil
}

// SaveSignoff records that a reviewer finished with a target.
func (s *Store) SaveSignoff(v domain.Signoff) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(),
		`INSERT INTO `+s.table("signoffs")+` (target_id, payload)
		 VALUES ($1, $2)
		 ON CONFLICT (target_id) DO UPDATE SET payload = EXCLUDED.payload, updated_at = now()`,
		v.TargetID, payload)
	return err
}

// LoadSignoff returns a target's verdict, or a zero Signoff when nobody has
// finished with it. Never reviewed is the ordinary state, not a failure.
func (s *Store) LoadSignoff(targetID string) (domain.Signoff, error) {
	var payload []byte
	err := s.pool.QueryRow(context.Background(),
		`SELECT payload FROM `+s.table("signoffs")+` WHERE target_id = $1`,
		targetID).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Signoff{}, nil
	}
	if err != nil {
		return domain.Signoff{}, err
	}
	var v domain.Signoff
	if err := json.Unmarshal(payload, &v); err != nil {
		// A corrupt verdict reads as "not reviewed", which is the safe way to
		// be wrong: it invites another look rather than claiming one happened.
		return domain.Signoff{}, nil
	}
	return v, nil
}

// SaveAnalysis stores one audit's result for one target, against the diff it
// was actually about.
func (s *Store) SaveAnalysis(a domain.Analysis) error {
	payload, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(context.Background(),
		`INSERT INTO `+s.table("analyses")+` (target_id, kind, print, payload)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (target_id, kind, print) DO UPDATE SET payload = EXCLUDED.payload, updated_at = now()`,
		a.TargetID, string(a.Kind), a.Print, payload)
	return err
}

// LoadAnalysis returns one audit's most recent result whatever diff it was
// about, or a zero Analysis when it has never been run. Never run is the
// ordinary state, not a failure.
func (s *Store) LoadAnalysis(targetID string, kind domain.AnalysisKind) (domain.Analysis, error) {
	return s.readAnalysis(
		`SELECT payload FROM `+s.table("analyses")+`
		 WHERE target_id = $1 AND kind = $2 ORDER BY updated_at DESC LIMIT 1`,
		targetID, string(kind))
}

// LoadAnalysisAt returns the result of one audit over one exact diff, or a zero
// Analysis when that diff has never been audited (ADR 0037).
func (s *Store) LoadAnalysisAt(targetID string, kind domain.AnalysisKind, print string) (domain.Analysis, error) {
	if print == "" {
		return domain.Analysis{}, nil
	}
	return s.readAnalysis(
		`SELECT payload FROM `+s.table("analyses")+`
		 WHERE target_id = $1 AND kind = $2 AND print = $3`,
		targetID, string(kind), print)
}

// readAnalysis runs one of the two queries above. No row reads as "never run",
// and so does a corrupt payload: both invite running it again rather than
// presenting something unreadable as a finding.
func (s *Store) readAnalysis(query string, args ...any) (domain.Analysis, error) {
	var payload []byte
	err := s.pool.QueryRow(context.Background(), query, args...).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Analysis{}, nil
	}
	if err != nil {
		return domain.Analysis{}, err
	}
	var a domain.Analysis
	if err := json.Unmarshal(payload, &a); err != nil {
		return domain.Analysis{}, nil
	}
	return a, nil
}

// Load reconstructs a session. The task prompt is the first prompt event's
// stated intent, matching the JSONL store's behaviour.
func (s *Store) Load(sessionID string) (domain.Session, error) {
	ctx := context.Background()
	sess := domain.Session{ID: sessionID}

	events, err := load[domain.Event](ctx, s, "events", sessionID)
	if err != nil {
		return domain.Session{}, err
	}
	sess.Events = events
	for _, e := range events {
		if e.Kind == domain.KindPrompt {
			sess.Prompt = e.StatedIntent
			break
		}
	}

	if sess.Units, err = load[domain.Unit](ctx, s, "units", sessionID); err != nil {
		return domain.Session{}, err
	}
	if sess.Notes, err = load[domain.Note](ctx, s, "notes", sessionID); err != nil {
		return domain.Session{}, err
	}
	if sess.Exchanges, err = load[domain.Exchange](ctx, s, "exchanges", sessionID); err != nil {
		return domain.Session{}, err
	}
	return sess, nil
}

// load reads a session's rows from one table in insertion order.
func load[T any](ctx context.Context, s *Store, table, sessionID string) ([]T, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT payload FROM `+s.table(table)+` WHERE session_id = $1 ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []T
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal(payload, &v); err != nil {
			continue // a malformed row is skipped, never fatal
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// nonZeroTime keeps a NOT NULL timestamp column satisfiable for records that
// never carried one.
func nonZeroTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return t
}
