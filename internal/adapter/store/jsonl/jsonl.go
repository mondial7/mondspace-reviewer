// Package jsonl is an append-only JSONL store. Each session's log lives under
// <root>/<session-id>/ as events.jsonl and units.jsonl: crash-safe, tail-able,
// trivially diffable in tests.
package jsonl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

type Store struct {
	root string
}

func New(root string) *Store {
	return &Store{root: root}
}

func (s *Store) AppendEvent(e domain.Event) error {
	return s.appendLine(e.SessionID, "events.jsonl", e)
}

func (s *Store) AppendUnit(u domain.Unit) error {
	return s.appendLine(u.SessionID, "units.jsonl", u)
}

// AppendExchange records one question and its answer, so the review
// conversation outlives the process that had it.
func (s *Store) AppendExchange(e domain.Exchange) error {
	return s.appendLine(e.SessionID, "ask.jsonl", e)
}

func (s *Store) AppendNote(n domain.Note) error {
	return s.appendLine(n.SessionID, "notes.jsonl", n)
}

// Load reconstructs a Session from its append-only files. The task prompt is
// the first prompt event's stated intent.
func (s *Store) Load(sessionID string) (domain.Session, error) {
	if err := validSessionID(sessionID); err != nil {
		return domain.Session{}, err
	}
	sess := domain.Session{ID: sessionID}

	events, err := readLines[domain.Event](filepath.Join(s.root, sessionID, "events.jsonl"))
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

	units, err := readLines[domain.Unit](filepath.Join(s.root, sessionID, "units.jsonl"))
	if err != nil {
		return domain.Session{}, err
	}
	sess.Units = units

	notes, err := readLines[domain.Note](filepath.Join(s.root, sessionID, "notes.jsonl"))
	if err != nil {
		return domain.Session{}, err
	}
	sess.Notes = notes

	exchanges, err := readLines[domain.Exchange](filepath.Join(s.root, sessionID, "ask.jsonl"))
	if err != nil {
		return domain.Session{}, err
	}
	sess.Exchanges = exchanges

	return sess, nil
}

// readLines decodes each JSON line of a file into T. A missing file yields no
// items. A malformed line is skipped, not fatal.
func readLines[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var items []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var v T
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			continue
		}
		items = append(items, v)
	}
	return items, sc.Err()
}

// validSessionID guards against path traversal: a session ID arrives from an
// agent hook payload and is used as a directory name, so it must be a single,
// benign path segment.
func validSessionID(sessionID string) error {
	if sessionID == "" || sessionID == "." || sessionID == ".." {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	if strings.ContainsAny(sessionID, `/\`) || strings.Contains(sessionID, "..") {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	return nil
}

func (s *Store) appendLine(sessionID, file string, v any) error {
	if err := validSessionID(sessionID); err != nil {
		return err
	}
	dir := filepath.Join(s.root, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, file), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// narrativeFile is the one file in the store that is rewritten rather than
// appended to: a session has one current story, not a history of them.
const narrativeFile = "narrative.json"

// SaveNarrative stores a session's story so it survives a restart. It is written
// to a temporary file and renamed, so a crash mid-write leaves the previous
// story intact rather than a truncated one.
func (s *Store) SaveNarrative(n domain.Narrative) error {
	dir := filepath.Join(s.root, n.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, narrativeFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, narrativeFile))
}

// LoadNarrative returns the stored story, or a zero Narrative when the session
// has never been narrated. That is an ordinary state, not a failure: the caller
// narrates it. Only a real I/O fault is an error.
func (s *Store) LoadNarrative(sessionID string) (domain.Narrative, error) {
	body, err := os.ReadFile(filepath.Join(s.root, sessionID, narrativeFile))
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Narrative{}, nil
	}
	if err != nil {
		return domain.Narrative{}, err
	}
	var n domain.Narrative
	if err := json.Unmarshal(body, &n); err != nil {
		// A corrupt story is worth re-narrating, not worth failing over.
		return domain.Narrative{}, nil
	}
	return n, nil
}

// signoffFile records that a reviewer finished with a target, and what they
// said about it as a whole. Rewritten rather than appended: a target has one
// current verdict, and re-signing replaces it (ADR 0021).
const signoffFile = "signoff.json"

// SaveSignoff stores a target's verdict so reopening it says so. Written to a
// temporary file and renamed, so a crash mid-write leaves the previous verdict
// intact rather than a truncated one.
func (s *Store) SaveSignoff(v domain.Signoff) error {
	dir := filepath.Join(s.root, v.TargetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, signoffFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, signoffFile))
}

// LoadSignoff returns a target's verdict, or a zero Signoff when nobody has
// finished with it. Never reviewed is the ordinary state, not a failure.
func (s *Store) LoadSignoff(targetID string) (domain.Signoff, error) {
	body, err := os.ReadFile(filepath.Join(s.root, targetID, signoffFile))
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Signoff{}, nil
	}
	if err != nil {
		return domain.Signoff{}, err
	}
	var v domain.Signoff
	if err := json.Unmarshal(body, &v); err != nil {
		// A corrupt verdict reads as "not reviewed", which is the safe way to
		// be wrong: it invites another look rather than claiming one happened.
		return domain.Signoff{}, nil
	}
	return v, nil
}

// analysisFile is where one audit's latest result lives, one file per kind.
//
// Per kind rather than one file holding all of them: two audits can be running
// at once, and a single file would mean read-modify-write races between two
// results that are supposed to be independent (ADR 0024).
func analysisFile(kind domain.AnalysisKind) string {
	return "analysis-" + string(kind) + ".json"
}

// analysisAtFile is where the result for one *particular* diff lives, so a
// reviewer who leaves a review and comes back to it unchanged is shown the
// answer they already paid for rather than being invited to buy it again
// (ADR 0037).
func analysisAtFile(kind domain.AnalysisKind, print string) string {
	return "analysis-" + string(kind) + "-" + print + ".json"
}

// keptPrints is how many past diffs' results are kept per audit. An audit only
// runs when somebody clicks, so this is generous; it exists so a review worked
// on all week does not accumulate a file per click forever.
const keptPrints = 12

// SaveAnalysis stores one audit's result for one target.
//
// It is written twice: once under the diff it was actually about, and once as
// the latest result for this audit whatever diff that was. The first is what
// makes coming back to an unchanged review free; the second is what lets a card
// show the previous answer while saying the code has moved since.
func (s *Store) SaveAnalysis(a domain.Analysis) error {
	dir := filepath.Join(s.root, a.TargetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(a)
	if err != nil {
		return err
	}

	if a.Print != "" {
		if err := writeFileAtomic(dir, analysisAtFile(a.Kind, a.Print), body); err != nil {
			return err
		}
		pruneAnalyses(dir, a.Kind)
	}
	return writeFileAtomic(dir, analysisFile(a.Kind), body)
}

// writeFileAtomic replaces one file in place, so a reader never sees a partial
// result — including a reader in another process, which is the ordinary case
// here.
func writeFileAtomic(dir, name string, body []byte) error {
	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// pruneAnalyses drops the oldest per-diff results for one audit, newest kept.
// A failure here is ignored: it is housekeeping, and losing the housekeeping is
// not a reason to fail a result that already ran.
func pruneAnalyses(dir string, kind domain.AnalysisKind) {
	prefix := "analysis-" + string(kind) + "-"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type aged struct {
		name string
		at   time.Time
	}
	var kept []aged
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		kept = append(kept, aged{e.Name(), info.ModTime()})
	}
	if len(kept) <= keptPrints {
		return
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].at.After(kept[j].at) })
	for _, old := range kept[keptPrints:] {
		os.Remove(filepath.Join(dir, old.name))
	}
}

// dismissalFile is where a reviewer's rulings on the deterministic findings
// live, one file per target.
//
// Only the rulings, not the findings. The findings themselves are reproduced by
// running the tools again — that is what makes them `reported` — so storing them
// would be storing a cache of something cheap beside the one thing that is not
// recoverable: what the human decided (ADR 0043).
const dismissalFile = "reported.json"

// SaveDismissals records what the reviewer made of the deterministic findings.
func (s *Store) SaveDismissals(targetID string, rulings map[string]domain.Verdict) error {
	dir := filepath.Join(s.root, targetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(rulings)
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, dismissalFile, body)
}

// LoadDismissals returns those rulings, keyed by finding. Nothing ruled on is
// the ordinary state, not a failure.
func (s *Store) LoadDismissals(targetID string) (map[string]domain.Verdict, error) {
	body, err := os.ReadFile(filepath.Join(s.root, targetID, dismissalFile))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]domain.Verdict{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]domain.Verdict{}
	if err := json.Unmarshal(body, &out); err != nil {
		// A corrupt file reads as "nothing ruled on". Losing a dismissal is bad;
		// refusing to show the review is worse.
		return map[string]domain.Verdict{}, nil
	}
	return out, nil
}

// foundFile is the last set of deterministic findings for a target.
//
// The findings are reproducible by running the tools again — that is what makes
// them `reported` — but only by a process that has those tools and that repository
// in front of it. The MCP server is a different process, `msr export` is another,
// and neither should have to shell out to nine linters to answer a question the
// review already knows the answer to. So the answer is written down beside the
// rulings, which are the part that is genuinely not recoverable (ADR 0043).
const foundFile = "reported-found.json"

// SaveReported records the findings themselves, for readers outside the process
// that produced them.
func (s *Store) SaveReported(targetID string, found []domain.Reported) error {
	dir := filepath.Join(s.root, targetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(found)
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, foundFile, body)
}

// LoadReported reads them back. Nothing scanned is the ordinary state.
func (s *Store) LoadReported(targetID string) ([]domain.Reported, error) {
	body, err := os.ReadFile(filepath.Join(s.root, targetID, foundFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []domain.Reported
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, nil
	}
	return out, nil
}

// LoadAnalysisAt returns the result of one audit over one exact diff, or a zero
// Analysis when that diff has never been audited.
//
// The latest file is consulted too, because it may itself be the result for
// this diff — which is the case for every result written before results were
// kept per diff at all.
func (s *Store) LoadAnalysisAt(targetID string, kind domain.AnalysisKind, print string) (domain.Analysis, error) {
	if print == "" {
		return domain.Analysis{}, nil
	}
	if a, err := s.readAnalysis(filepath.Join(s.root, targetID, analysisAtFile(kind, print))); err == nil && a.Done() {
		return a, nil
	}
	a, err := s.LoadAnalysis(targetID, kind)
	if err != nil || a.Print != print {
		return domain.Analysis{}, err
	}
	return a, nil
}

// LoadAnalysis returns one audit's most recent result whatever diff it was
// about, or a zero Analysis when it has never been run. Never run is the
// ordinary state, not a failure.
func (s *Store) LoadAnalysis(targetID string, kind domain.AnalysisKind) (domain.Analysis, error) {
	return s.readAnalysis(filepath.Join(s.root, targetID, analysisFile(kind)))
}

// readAnalysis is one stored result. A missing or corrupt file reads as "never
// run", which invites running it again rather than presenting something
// unreadable as a finding.
func (s *Store) readAnalysis(path string) (domain.Analysis, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Analysis{}, nil
	}
	if err != nil {
		return domain.Analysis{}, err
	}
	var a domain.Analysis
	if err := json.Unmarshal(body, &a); err != nil {
		return domain.Analysis{}, nil
	}
	return a, nil
}
