package domain

// Workload is one of the jobs the reviewer's assistant is asked to do. They
// have genuinely different shapes, and the model that suits one need not suit
// another (ADR 0019):
//
//   - Narration is the hard call — a large schema whose group names are an enum
//     of real paths, so it needs the strongest grammar adherence available.
//   - Describe is short, schema-constrained and high-volume, so latency is what
//     matters and a small model pays for itself many times per review.
//   - Ask is free prose over a bounded context, and is the only one a person
//     waits on with nothing else to read.
type Workload string

const (
	Narration Workload = "narration"
	Describe  Workload = "describe"
	Ask       Workload = "ask"
)

// Workloads is every workload, for iterating over configuration and status.
var Workloads = []Workload{Narration, Describe, Ask}

// ModelRef names a model and where it is served. Either field may be empty,
// which means "whatever the default is" rather than "nothing".
type ModelRef struct {
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
}

// For resolves which model answers a workload.
//
// Three sources, most deliberate first. An override is the reviewer saying so,
// and wins outright. Otherwise the routing table decides, and a workload it
// sends to the CLI goes there whenever the CLI is configured. Everything else
// is the shared local model.
//
// Overrides fall back field by field, not whole. Two models behind one
// llama-server is a real arrangement — same port, different model name — and
// making the endpoint be repeated in order to change the model only invites it
// being repeated wrongly.
func (c AgentConfig) For(w Workload) ModelRef {
	out := c.Local()
	if EngineOn(w) == EngineCLI && c.UsesCLI() {
		out = c.CLI
	}
	over, ok := c.Overrides[w]
	if !ok {
		return out
	}
	if over.Endpoint != "" {
		out.Endpoint = over.Endpoint
	}
	if over.Model != "" {
		out.Model = over.Model
	}
	return out
}

// Local is the shared model: the endpoint and model the reviewer configured for
// the engine running on their own machine.
func (c AgentConfig) Local() ModelRef { return ModelRef{Endpoint: c.Endpoint, Model: c.Model} }

// Split reports whether more than one model is actually answering. An override
// that repeats the default is not a split: the status page has to say how many
// models are in play, and that is easy to get wrong by eye.
func (c AgentConfig) Split() bool {
	base := c.Local()
	for _, w := range Workloads {
		if c.For(w) != base {
			return true
		}
	}
	return false
}
