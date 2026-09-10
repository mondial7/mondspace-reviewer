// Package auto persists what auto-mode is allowed to do, what it has already
// done, and every decision it took (ADR 0047).
//
// Three files, because they have three different authors. The policy is edited
// by a person, the state is written by msr, and the log is append-only
// evidence. Putting them in one file would mean rewriting a human's file to
// record a machine's counter.
package auto

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/usecase/judge"
)

const (
	// PolicyFile is the human's half: what auto-mode may do.
	PolicyFile = "auto.toml"
	// StateFile is msr's half: what it has done this session.
	StateFile = "auto-state.json"
	// LogFile is every decision, accepted or rejected, with its reason.
	LogFile = "judge.jsonl"
)

// Store is the three files, under the shared directory.
type Store struct{ dir string }

func New(dir string) *Store { return &Store{dir: dir} }

// policyDoc is the policy as it is written down. Durations are strings because
// "5m" is what a person types and 300000000000 is not.
type policyDoc struct {
	Auto struct {
		Enabled     bool   `toml:"enabled"`
		MaxPushes   int    `toml:"max_pushes"`
		Cooldown    string `toml:"cooldown"`
		MinSeverity string `toml:"min_severity"`
		MaxPerBatch int    `toml:"max_per_batch"`
	} `toml:"auto"`
}

// Policy reads what auto-mode is allowed to do.
//
// No file is the default policy, which is off. A file that cannot be parsed is
// an error rather than a silent fallback: somebody wrote bounds down, and
// running with different ones because their syntax was wrong is the worst
// possible reading of it.
func (s *Store) Policy() (judge.Policy, error) {
	body, err := os.ReadFile(filepath.Join(s.dir, PolicyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return judge.DefaultPolicy(), nil
	}
	if err != nil {
		return judge.Policy{}, err
	}

	var doc policyDoc
	if err := toml.Unmarshal(body, &doc); err != nil {
		return judge.Policy{}, err
	}

	policy := judge.DefaultPolicy()
	policy.Enabled = doc.Auto.Enabled
	if doc.Auto.MaxPushes > 0 {
		policy.MaxPushes = doc.Auto.MaxPushes
	}
	if doc.Auto.MaxPerBatch > 0 {
		policy.MaxPerBatch = doc.Auto.MaxPerBatch
	}
	if doc.Auto.MinSeverity != "" {
		policy.MinSeverity = severity(doc.Auto.MinSeverity)
	}
	if doc.Auto.Cooldown != "" {
		cooldown, err := time.ParseDuration(doc.Auto.Cooldown)
		if err != nil {
			return judge.Policy{}, err
		}
		policy.Cooldown = cooldown
	}
	return policy, nil
}

// SavePolicy writes the policy back, which is what `msr auto on` does.
func (s *Store) SavePolicy(policy judge.Policy) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	var doc policyDoc
	doc.Auto.Enabled = policy.Enabled
	doc.Auto.MaxPushes = policy.MaxPushes
	doc.Auto.MaxPerBatch = policy.MaxPerBatch
	doc.Auto.MinSeverity = string(policy.MinSeverity)
	doc.Auto.Cooldown = policy.Cooldown.String()

	f, err := os.Create(filepath.Join(s.dir, PolicyFile))
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(doc)
}

// State reads what auto-mode has done in this session. A different session id
// starts the counts again, which is what makes a per-session budget mean
// anything.
func (s *Store) State(session string) (judge.State, error) {
	state, err := s.stored()
	if err != nil {
		return judge.State{}, err
	}
	if state.Session != session {
		// A different run: the counts start again, which is what makes a
		// per-session budget mean anything. A suspension does not, because it
		// is the human saying stop and they did not say it about a session id.
		return judge.State{Session: session, Suspended: state.Suspended}, nil
	}
	return state, nil
}

// SaveState writes it back.
func (s *Store) SaveState(state judge.State) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, StateFile), body, 0o644)
}

// SetSuspended stands the running session down, or lifts it, without needing to
// know which session that is.
//
// `msr push` is the other half of auto-mode's kill switch (ADR 0047) and it
// does not know the agent's session id — that belongs to whoever calls
// `msr auto run`. Reading the state under the wrong id hands back a zero one,
// and writing that back did two things, both wrong: the running session was not
// suspended, and the budget it had already spent was reset to nothing. So this
// reads whatever is stored, changes the one flag, and puts it back.
func (s *Store) SetSuspended(suspended bool) error {
	state, err := s.stored()
	if err != nil {
		return err
	}
	if state.Suspended == suspended {
		return nil
	}
	state.Suspended = suspended
	return s.SaveState(state)
}

// stored is the state as written, whatever session it names.
func (s *Store) stored() (judge.State, error) {
	body, err := os.ReadFile(filepath.Join(s.dir, StateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return judge.State{}, nil
	}
	if err != nil {
		return judge.State{}, err
	}
	var state judge.State
	if err := json.Unmarshal(body, &state); err != nil {
		// A state nobody can read is a state nobody has: the counts start
		// again, which is the conservative direction for a budget.
		return judge.State{}, nil
	}
	return state, nil
}

// Log appends every decision this run took.
//
// Rejections included. A log of what was pushed says nothing about the
// judgement that produced it; a log of what was considered says everything.
func (s *Store) Log(decisions ...judge.Decision) error {
	if len(decisions) == 0 {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, LogFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, decision := range decisions {
		line, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return w.Flush()
}

// Decisions reads the log back, for `msr auto status` and for anybody asking
// why a finding was or was not sent.
func (s *Store) Decisions() ([]judge.Decision, error) {
	f, err := os.Open(filepath.Join(s.dir, LogFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []judge.Decision
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var decision judge.Decision
		if err := json.Unmarshal(sc.Bytes(), &decision); err != nil {
			continue
		}
		out = append(out, decision)
	}
	return out, sc.Err()
}

// severity keeps an unreadable floor from becoming a permissive one: an
// unknown word normalises to medium rather than to nothing at all.
func severity(word string) contract.Severity {
	return contract.Severity(word).Normalise()
}
