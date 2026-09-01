#!/bin/sh
# Builds the repository the screenshots in this directory were taken against.
#
# Two commits. The first is a small service that works; the second adds bearer
# auth and Postgres-backed sessions, and does it the way a hurried afternoon
# does it — a token concatenated into a SQL string, an exported signature
# changed, an error dropped, a dependency taken on, no tests. Every finding on
# those screenshots is a real finding about this code.
#
#   sh docs/img/demo.sh /tmp/ledger
#   msr web --repo=/tmp/ledger --out=/tmp/ledger-store --addr=127.0.0.1:7777
#
# Then open the second commit, press start review, run both audits, and capture
# as described in README.md.
set -eu

dir=${1:?usage: demo.sh <directory>}
mkdir -p "$dir"/auth "$dir"/api "$dir"/store
cd "$dir"

cat > go.mod <<'EOF'
module github.com/example/ledger

go 1.25
EOF

cat > README.md <<'EOF'
# ledger

A small HTTP service that records entries. Three packages: `auth` decides who is
asking, `api` routes, `store` persists.
EOF

cat > api/handler.go <<'EOF'
package api

import (
	"encoding/json"
	"net/http"

	"github.com/example/ledger/store"
)

// Routes wires the service.
func Routes(entries *store.Entries) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /entries", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(entries.All())
	})
	return mux
}
EOF

cat > store/entries.go <<'EOF'
package store

import "sync"

// Entry is one line in the ledger.
type Entry struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

// Entries holds them in memory.
type Entries struct {
	mu   sync.RWMutex
	kept []Entry
}

// All returns every entry.
func (e *Entries) All() []Entry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.kept
}

// Add records one.
func (e *Entries) Add(entry Entry) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.kept = append(e.kept, entry)
}
EOF

cat > store/entries_test.go <<'EOF'
package store

import "testing"

func TestAddThenAll(t *testing.T) {
	var e Entries
	e.Add(Entry{ID: "a", Amount: 1})
	if got := e.All(); len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
}
EOF

cat > auth/auth.go <<'EOF'
package auth

import "net/http"

// Anyone lets every request through. The service is not exposed yet.
func Anyone(next http.Handler) http.Handler {
	return next
}
EOF

git init -q .
git config user.email dev@example.com
git config user.name "A developer"
git add -A
GIT_AUTHOR_DATE=2026-08-29T09:14:00 GIT_COMMITTER_DATE=2026-08-29T09:14:00 \
	git commit -qm "Record entries over HTTP, in memory for now"

# The second commit: the one every screenshot is a review of.
cat > auth/auth.go <<'EOF'
package auth

import (
	"net/http"
	"strings"
)

// Bearer checks the token on every request.
//
// Replaces Anyone, which let everything through while the service was not
// exposed. It is exposed now.
func Bearer(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
EOF

cat > store/sessions.go <<'EOF'
package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Sessions keeps who is signed in, in Postgres, so a restart does not sign
// everybody out.
type Sessions struct {
	db *sql.DB
}

// NewSessions opens the table.
func NewSessions(db *sql.DB) *Sessions {
	return &Sessions{db: db}
}

// Lookup finds a session by its token.
func (s *Sessions) Lookup(token string) (string, time.Time, error) {
	row := s.db.QueryRow(
		"SELECT user_id, expires_at FROM sessions WHERE token = '" + token + "'")

	var user string
	var expires time.Time
	if err := row.Scan(&user, &expires); err != nil {
		return "", time.Time{}, fmt.Errorf("no such session")
	}
	return user, expires, nil
}

// Forget ends a session.
func (s *Sessions) Forget(token string) {
	_, _ = s.db.Exec("DELETE FROM sessions WHERE token = $1", token)
}
EOF

cat > api/handler.go <<'EOF'
package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/example/ledger/auth"
	"github.com/example/ledger/store"
)

// Routes wires the service.
//
// It now takes the session store, because a request has to say who it is
// before it sees anybody's ledger.
func Routes(entries *store.Entries, sessions *store.Sessions, secret string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /entries", auth.Bearer(secret,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			user, _, err := sessions.Lookup(token)
			if err != nil {
				return
			}
			log.Printf("serving %s for %s", r.URL.Path, user)
			json.NewEncoder(w).Encode(entries.All())
		})))
	return mux
}
EOF

printf '\nrequire github.com/lib/pq v1.10.9\n' >> go.mod

git add -A
GIT_AUTHOR_DATE=2026-09-01T14:32:00 GIT_COMMITTER_DATE=2026-09-01T14:32:00 \
	git commit -qm "Ask who is calling, and remember it across a restart"

# A remote, so the branches page and the history card have something to show:
# two branches nobody has merged, and one commit a colleague pushed after you.
remote="$dir.git"
rm -rf "$remote"
git init -q --bare "$remote"
git remote add origin "$remote"
git push -q origin main
git branch -q --set-upstream-to=origin/main main

git checkout -qb rate-limit-the-api
printf 'package api\n\nimport "time"\n\n// Window is how long a caller'"'"'s allowance lasts.\nconst Window = time.Minute\n' > api/limit.go
git add -A
GIT_AUTHOR_DATE=2026-09-01T16:02:00 GIT_COMMITTER_DATE=2026-09-01T16:02:00 \
	git commit -qm "Start on a per-caller rate limit"
git push -q origin rate-limit-the-api
git checkout -q main

git checkout -qb retire-the-memory-store
printf '\nThe in-memory store is going away once sessions land.\n' >> README.md
git add -A
GIT_AUTHOR_DATE=2026-09-01T17:40:00 GIT_COMMITTER_DATE=2026-09-01T17:40:00 \
	git commit -qm "Note that the memory store is on its way out"
git push -q origin retire-the-memory-store
git checkout -q main

colleague="$dir-colleague"
rm -rf "$colleague"
git clone -q "$remote" "$colleague"
(
	cd "$colleague"
	git config user.email sam@example.com
	git config user.name Sam
	printf 'package store\n\nimport (\n\t"database/sql"\n\t"time"\n)\n\n// Pool bounds how many connections the service holds open.\nfunc Pool(db *sql.DB) {\n\tdb.SetMaxOpenConns(16)\n\tdb.SetConnMaxLifetime(5 * time.Minute)\n}\n' > store/pool.go
	git add -A
	GIT_AUTHOR_DATE=2026-09-01T18:20:00 GIT_COMMITTER_DATE=2026-09-01T18:20:00 \
		git commit -qm "Bound the connection pool before this reaches staging"
	git push -q origin main
)
rm -rf "$colleague"
git fetch -q origin

echo "built $dir — review $(git rev-parse --short HEAD)"
