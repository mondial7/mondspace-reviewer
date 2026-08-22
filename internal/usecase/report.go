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

	return r
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
