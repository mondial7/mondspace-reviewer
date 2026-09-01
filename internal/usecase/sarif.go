package usecase

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// Two decoders, and only two (ADR 0043).
//
// SARIF is the reason a tool can be added with configuration alone: it is the
// interchange format the analyser world settled on, and anything emitting it is
// already understood. "lines" is the fallback for everything else, because the
// tools that do not emit SARIF all emit the same thing — `file:line:col:
// message` — and have done for forty years.
//
// A third decoder would be a third thing to keep working. Where one is wanted,
// the answer is a tool flag that produces SARIF.

// ReadFindings decodes one analyser's output into findings attributed to it.
//
// Output that cannot be read yields nothing rather than an error. A tool that
// printed a warning on stdout, or a version banner, or nothing at all, must not
// take a review down; the settings page reports what could not be read
// (ADR 0043).
func ReadFindings(a Analyser, output, repoDir string) []domain.Reported {
	switch a.Format {
	case FormatSARIF:
		return readSARIF(a, output, repoDir)
	default:
		return readLines(a, output, repoDir)
	}
}

// sarifLog is the part of SARIF msr reads. It is a large specification and this
// is deliberately a small corner of it: where, what rule, what was said, how
// bad. Everything else in the document is for a different kind of consumer.
type sarifLog struct {
	Runs []struct {
		Tool struct {
			Driver struct {
				Name string `json:"name"`
			} `json:"driver"`
		} `json:"tool"`
		Results []struct {
			RuleID  string `json:"ruleId"`
			Level   string `json:"level"`
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
			Locations []struct {
				PhysicalLocation struct {
					ArtifactLocation struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
					Region struct {
						StartLine int `json:"startLine"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
		} `json:"results"`
	} `json:"runs"`
}

func readSARIF(a Analyser, output, repoDir string) []domain.Reported {
	body := strings.TrimSpace(output)
	// Some tools print a line of their own before the document. Finding the
	// object is the same defensive move the model replies get.
	if i := strings.Index(body, "{"); i > 0 {
		body = body[i:]
	}

	var log sarifLog
	if err := json.Unmarshal([]byte(body), &log); err != nil {
		return nil
	}

	var out []domain.Reported
	for _, run := range log.Runs {
		for _, res := range run.Results {
			message := strings.TrimSpace(res.Message.Text)
			if message == "" {
				continue
			}
			file, line := "", 0
			if len(res.Locations) > 0 {
				loc := res.Locations[0].PhysicalLocation
				file = relativeTo(repoDir, loc.ArtifactLocation.URI)
				line = loc.Region.StartLine
			}
			out = append(out, domain.Reported{
				// The analyser's own name, not the driver's. They usually agree;
				// where they do not, the reviewer configured this entry and it is
				// their word for the tool that should appear beside the finding.
				Tool:     a.Name,
				Rule:     strings.TrimSpace(res.RuleID),
				File:     file,
				Line:     line,
				Message:  Brief(message, reportedChars),
				Severity: a.Level(res.Level),
			})
		}
	}
	return out
}

// lineFinding matches the convention every tool that is not emitting SARIF
// follows: a path, a line, an optional column, and a message.
//
// The trailing `(RULE)` is staticcheck's and ruff's way of naming the check,
// and the leading `[rule]` is eslint's under `--format unix`. Both are pulled
// out, because a finding that cannot name its rule is not `reported` — it is
// just a sentence (ADR 0043).
var lineFinding = regexp.MustCompile(`^(.+?):(\d+)(?::(\d+))?:\s*(.*)$`)

var trailingRule = regexp.MustCompile(`\s*\(([A-Za-z][\w./-]*)\)\s*$`)

func readLines(a Analyser, output, repoDir string) []domain.Reported {
	var out []domain.Reported
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := lineFinding.FindStringSubmatch(line)
		if m == nil {
			// `go vet` prefixes a package header line, and every tool prints
			// something that is not a finding sooner or later.
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}

		message, rule := strings.TrimSpace(m[4]), ""
		if hit := trailingRule.FindStringSubmatch(message); hit != nil {
			rule = hit[1]
			message = strings.TrimSpace(message[:len(message)-len(hit[0])])
		}
		if message == "" {
			continue
		}
		out = append(out, domain.Reported{
			Tool:    a.Name,
			Rule:    rule,
			File:    relativeTo(repoDir, m[1]),
			Line:    n,
			Message: Brief(message, reportedChars),
			// Nothing said how bad it is, so nothing is claimed. Normalise puts
			// it at "worth checking", which is the honest answer.
			Severity: a.Level(""),
		})
	}
	return out
}

// reportedChars bounds a tool's sentence. Analysers are terse and this almost
// never bites; it is here because nothing from a subprocess should reach the
// page unbounded.
const reportedChars = 240

// relativeTo turns whatever a tool called a file into the repository-relative,
// forward-slashed path that git, the units and the annotations all use.
//
// Tools disagree: SARIF says `file:///abs/path`, gosec prints an absolute path,
// `go vet` prints one relative to the package. Getting this wrong does not
// produce an error — it produces a finding that silently attaches to no file,
// which is the worst kind of wrong here.
func relativeTo(repoDir, path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "file://")
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(path)

	if repoDir != "" {
		root := filepath.ToSlash(repoDir)
		if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = filepath.ToSlash(rel)
		}
	}
	return strings.TrimPrefix(strings.TrimPrefix(path, "./"), "/")
}
