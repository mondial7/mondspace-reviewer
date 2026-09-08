// Package legacy reads what older builds of msr wrote.
//
// It exists so that the two stores do not each carry their own copy of the
// same archaeology, and so that there is one obvious place to delete from when
// a format is old enough to stop supporting.
package legacy

import (
	"encoding/json"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
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

// Note puts back the three fields a reviewer's annotation was written with
// before notes became items (ADR 0048): the text, the time and the file.
//
// This is the one migration that cannot be got wrong. An analyser's cache is
// re-derived and a model's reading can be re-run; a note is something a person
// typed once, and there is nowhere else to get it from. Everything else about
// the record — the id, the session, the unit, the kind, the anchor, the
// supersession — kept its name and decodes without help.
func Note(item contract.Item, raw []byte) contract.Item {
	var stored struct {
		Text string    `json:"text"`
		TS   time.Time `json:"ts"`
		File string    `json:"file"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return item
	}

	if item.Message == "" {
		item.Message = stored.Text
	}
	if item.Directive == "" {
		item.Directive = item.Message
	}
	if item.FirstSeen.IsZero() {
		item.FirstSeen = stored.TS
	}
	if item.LastSeen.IsZero() {
		item.LastSeen = item.FirstSeen
	}
	if item.Location.Path == "" {
		item.Location.Path = stored.File
	}
	if item.Source == "" {
		item.Source = contract.SourceHuman
	}
	return item
}
