package usecase

import "github.com/marcomondini/mondspace-reviewer/internal/domain"

// groupOrder fixes the order note kinds appear in the review report.
var groupOrder = []domain.NoteKind{
	domain.NoteOK, domain.NoteQuestion, domain.NoteObjection, domain.NoteDebt, domain.NoteNote,
}

// BuildReport projects a session into an exportable review. It is pure: notes
// are re-checked for supersession from the current units, then bucketed.
func BuildReport(sess domain.Session) domain.Report {
	notes := MarkSuperseded(sess.Units, sess.Notes)
	headlines := map[string]domain.Headline{}
	for _, u := range sess.Units {
		headlines[u.ID] = u.Headline
	}

	r := domain.Report{SessionID: sess.ID, Prompt: sess.Prompt}

	for _, kind := range groupOrder {
		var items []domain.ReportItem
		for _, n := range notes {
			if n.Kind == kind {
				items = append(items, itemFor(n, headlines[n.UnitID]))
			}
		}
		if len(items) > 0 {
			r.Groups = append(r.Groups, domain.NoteGroup{Kind: kind, Items: items})
		}
	}

	for _, n := range notes {
		item := itemFor(n, headlines[n.UnitID])
		switch {
		case n.Kind == domain.NoteDebt:
			r.Debt = append(r.Debt, item)
		case isOpen(n.Kind) && n.SupersededBy == "":
			r.Agenda = append(r.Agenda, item)
		case isOpen(n.Kind):
			r.Superseded = append(r.Superseded, item)
		}
	}

	annotated := map[string]bool{}
	for _, n := range notes {
		annotated[n.UnitID] = true
	}
	for _, u := range sess.Units {
		if !annotated[u.ID] {
			r.Unreviewed = append(r.Unreviewed, domain.ReportItem{UnitID: u.ID, Headline: u.Headline})
		}
	}

	return r
}

// isOpen reports whether a note kind represents an unresolved concern.
func isOpen(kind domain.NoteKind) bool {
	return kind == domain.NoteQuestion || kind == domain.NoteObjection
}

func itemFor(n domain.Note, h domain.Headline) domain.ReportItem {
	return domain.ReportItem{
		UnitID:       n.UnitID,
		Headline:     h,
		NoteKind:     n.Kind,
		NoteText:     n.Text,
		SupersededBy: n.SupersededBy,
	}
}
