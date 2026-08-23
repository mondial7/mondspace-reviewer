package domain

// Report is the exportable review: annotated units grouped by note kind, the
// debt task list, the live agenda, superseded items, and unreviewed units. It
// is a pure projection of the session.
type Report struct {
	SessionID  string       `json:"session_id"`
	Prompt     string       `json:"prompt"`
	Groups     []NoteGroup  `json:"groups"`
	Debt       []ReportItem `json:"debt"`
	Agenda     []ReportItem `json:"agenda"`
	Superseded []ReportItem `json:"superseded"`
	Unreviewed []ReportItem `json:"unreviewed"`
}

// NoteGroup is the annotated units sharing one note kind.
type NoteGroup struct {
	Kind  NoteKind     `json:"kind"`
	Items []ReportItem `json:"items"`
}

// ReportItem is one annotated (or unreviewed) unit in the report.
type ReportItem struct {
	UnitID       string   `json:"unit_id"`
	Headline     Headline `json:"headline"`
	Flags        []Flag   `json:"flags,omitempty"`
	NoteKind     NoteKind `json:"note_kind,omitempty"`
	NoteText     string   `json:"note_text,omitempty"`
	SupersededBy string   `json:"superseded_by,omitempty"`
}
