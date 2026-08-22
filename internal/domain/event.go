package domain

import (
	"encoding/json"
	"time"
)

type Kind string

const (
	KindEdit     Kind = "edit"
	KindWrite    Kind = "write"
	KindBash     Kind = "bash"
	KindPrompt   Kind = "prompt"
	KindBatchEnd Kind = "batch_end"
)

type Event struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"session_id"`
	TS           time.Time       `json:"ts"`
	Source       string          `json:"source"`
	Kind         Kind            `json:"kind"`
	Tool         string          `json:"tool"`
	Files        []string        `json:"files"`
	StatedIntent string          `json:"stated_intent"`
	Raw          json.RawMessage `json:"raw"`
}
