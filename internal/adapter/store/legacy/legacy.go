// Package legacy reads what older builds of msr wrote.
//
// It exists so that the two stores do not each carry their own copy of the
// same archaeology, and so that there is one obvious place to delete from when
// a format is old enough to stop supporting.
package legacy

import (
	"encoding/json"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// Findings puts back the two fields a model's finding was written with before
// findings became items (ADR 0048).
//
// The analyser cache can be thrown away and re-derived; this cannot. A reading
// costs a model run, and the verdicts on it are a human's. Severity and verdict
// decode as they always did — the file and the sentence are the only things
// that moved, so they are the only things restored.
func Findings(a domain.Analysis, body []byte) domain.Analysis {
	var stored struct {
		Findings []struct {
			File string `json:"file"`
			Note string `json:"note"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(body, &stored); err != nil {
		return a
	}

	for i := range a.Findings {
		if i >= len(stored.Findings) {
			break
		}
		if a.Findings[i].Location.Path == "" {
			a.Findings[i].Location.Path = stored.Findings[i].File
		}
		if a.Findings[i].Message == "" {
			a.Findings[i].Message = stored.Findings[i].Note
		}
		if a.Findings[i].Directive == "" {
			a.Findings[i].Directive = a.Findings[i].Message
		}
	}
	return a
}
