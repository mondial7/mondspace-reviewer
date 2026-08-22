package domain

// WhySrc distinguishes a rationale taken verbatim from the agent ("stated")
// from one guessed by the summarizer ("inferred"). It must be unambiguous in
// every rendering; when in doubt, inferred.
const (
	WhyStated   = "stated"
	WhyInferred = "inferred"
)

type Headline struct {
	Text   string `json:"text"`
	Why    string `json:"why"`
	WhySrc string `json:"why_src"`
}
