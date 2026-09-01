package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// AnalyserFile is where a repository says which deterministic analysers to run
// over it (ADR 0043).
//
// In the repository rather than in msr's own config directory, because which
// linters a project uses is a fact about the project. It is checked in, and
// everybody reviewing that repository gets the same fourth reading.
const AnalyserFile = ".msr.toml"

// analyserDoc is the shape of that file.
//
// One table array and one switch. There is deliberately nothing else in it: the
// moment this grows a second section it becomes a general settings file, and
// msr's position is that a value which can be recomputed should not be stored.
type analyserDoc struct {
	// Analysers replaces the built-in defaults when it is present. Not merged:
	// a reviewer who lists three tools means those three, and a merge would
	// silently keep running the six they did not mention.
	Analysers []usecase.Analyser `toml:"analyser"`
	// Extra is added to the defaults rather than replacing them, which is what
	// somebody adding one house tool to the usual set actually wants.
	Extra []usecase.Analyser `toml:"extra"`
	// Off names built-in analysers to leave alone. A repository that has gosec
	// installed for CI and does not want it in a five-second poll says so here.
	Off []string `toml:"off"`
}

// LoadAnalysers reads a repository's analyser configuration and returns the set
// to run.
//
// No file is not an error: running the built-in defaults is the normal case,
// and on a machine with none of those tools installed it produces nothing and
// says nothing. A file that exists and cannot be parsed *is* an error — silently
// falling back to the defaults while the file says otherwise leaves nothing to
// explain why a tool a reviewer configured never ran.
func LoadAnalysers(repoDir string) ([]usecase.Analyser, error) {
	body, err := os.ReadFile(filepath.Join(repoDir, AnalyserFile))
	if errors.Is(err, fs.ErrNotExist) {
		return usecase.BuiltInAnalysers(), nil
	}
	if err != nil {
		return nil, err
	}

	var doc analyserDoc
	if err := toml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("reading %s: %w", AnalyserFile, err)
	}

	base := usecase.BuiltInAnalysers()
	if len(doc.Analysers) > 0 {
		base = doc.Analysers
	}

	off := map[string]bool{}
	for _, name := range doc.Off {
		off[strings.TrimSpace(name)] = true
	}

	out := make([]usecase.Analyser, 0, len(base)+len(doc.Extra))
	for _, a := range base {
		if off[a.Name] {
			continue
		}
		out = append(out, a)
	}
	for _, a := range doc.Extra {
		if off[a.Name] {
			continue
		}
		out = append(out, a)
	}

	for i, a := range out {
		if err := valid(a); err != nil {
			return nil, fmt.Errorf("%s: analyser %d: %w", AnalyserFile, i+1, err)
		}
	}
	return out, nil
}

// valid rejects a definition that could not possibly work.
//
// Loudly, at load, rather than quietly at run time. A misspelled format or a
// missing detect command produces a tool that silently never reports anything,
// which is the failure mode this whole layer is least able to survive: a
// reviewer cannot tell "no findings" from "never ran".
func valid(a usecase.Analyser) error {
	switch {
	case strings.TrimSpace(a.Name) == "":
		return fmt.Errorf("needs a name")
	case len(a.Detect) == 0:
		return fmt.Errorf("%s: needs a detect command, or msr cannot tell whether it is installed", a.Name)
	case len(a.Run) == 0:
		return fmt.Errorf("%s: needs a run command", a.Name)
	case a.Format != usecase.FormatSARIF && a.Format != usecase.FormatLines:
		return fmt.Errorf("%s: format is %q; msr reads %q and %q",
			a.Name, a.Format, usecase.FormatSARIF, usecase.FormatLines)
	}
	return nil
}
