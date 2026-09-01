package domain

// This file is the routing table. Where a question goes is decided here and
// nowhere else: the point of writing it as data is that "which model reads my
// security pass" is answered by reading one list rather than by tracing
// conditionals through the analysis code (ADR 0039).

// Engine is one of the two things that can answer msr's questions.
type Engine string

const (
	// EngineCLI is the Claude Code CLI on this machine, run with no tools and
	// fed on stdin (ADR 0035). It costs whatever the reviewer's subscription
	// costs, and it is much better at the judgement calls.
	EngineCLI Engine = "claude cli"
	// EngineLocal is the model the reviewer started — an OpenAI-compatible
	// endpoint, usually llama-server with a small instruct model. Free,
	// offline, fast, and not to be trusted with a verdict.
	EngineLocal Engine = "local model"
)

// ClaudeCLIEndpoint is what a reviewer types where an endpoint would go, and
// what the routing below resolves to for the jobs that want the CLI.
const ClaudeCLIEndpoint = "claude://cli"

// Job is one thing msr asks a model to do.
//
// Finer than a Workload on purpose. "The security pass" and "the story" are
// routed the same way but they are not the same question, and a table that
// cannot name them cannot be read as the answer to "where does this go".
type Job string

const (
	JobStory    Job = "story"
	JobSecurity Job = "security pass"
	JobBreaking Job = "breaking changes"
	JobGroup    Job = "group descriptions"
	JobFile     Job = "file descriptions"
	JobAsk      Job = "questions"
)

// Route is one row: what the job is, which configured model answers it, the
// engine it is meant for, what answers when that engine is not there, and why
// it was put where it was.
type Route struct {
	Job      Job
	Workload Workload
	Engine   Engine
	// Fallback is empty when there is nowhere else to go. A job routed to the
	// local model has no second engine behind it: the CLI charges per call and
	// the high-volume jobs are exactly the ones that must not.
	Fallback Engine
	Why      string
}

// Routing is the whole table.
//
// The shape of it is one sentence: judgement goes to the CLI, volume stays
// local. The three readings a reviewer acts on are the three a small local model
// gets wrong in the way that costs the most — a security card that invents a
// finding sends somebody to look at nothing, and twice is enough for the card to
// stop being read (ADR 0035). The per-file and per-group descriptions are the
// opposite: dozens per review, low stakes, and a paid call each would be a bill
// nobody asked for.
var Routing = []Route{
	{JobStory, Narration, EngineCLI, EngineLocal,
		"a story is a judgement about what a change was for"},
	{JobSecurity, Narration, EngineCLI, EngineLocal,
		"an invented finding costs a reviewer more than a missing one"},
	{JobBreaking, Narration, EngineCLI, EngineLocal,
		"telling an addition from a change is what the small model cannot do"},
	{JobGroup, Describe, EngineLocal, "",
		"one call per group, several per review — volume, not judgement"},
	{JobFile, Describe, EngineLocal, "",
		"one call per changed file: the highest-volume thing msr does"},
	{JobAsk, Ask, EngineLocal, "",
		"a person is waiting on this one with nothing else to read"},
}

// RouteFor is one job's row.
func RouteFor(j Job) (Route, bool) {
	for _, r := range Routing {
		if r.Job == j {
			return r, true
		}
	}
	return Route{}, false
}

// JobsOn is every job a workload answers, so the settings page can say what a
// configured model is actually being asked to do.
func JobsOn(w Workload) []Job {
	var out []Job
	for _, r := range Routing {
		if r.Workload == w {
			out = append(out, r.Job)
		}
	}
	return out
}

// EngineOn is the engine a workload's jobs are routed to.
//
// Every job on one workload must agree, because a workload is the unit the
// reviewer configures and the unit a summarizer is built for. The day two jobs
// on one workload want different engines is the day the workload has to split;
// until then this is the honest way to read the table from the runtime, and the
// first row wins so a mistake shows up as the wrong engine rather than a panic.
func EngineOn(w Workload) Engine {
	for _, r := range Routing {
		if r.Workload == w {
			return r.Engine
		}
	}
	return EngineLocal
}

// FallbackOn is what answers a workload when its engine is not there. Empty
// means nothing does, and the caller degrades to whatever it does without a
// model at all — a mechanical headline, an undescribed group.
func FallbackOn(w Workload) Engine {
	for _, r := range Routing {
		if r.Workload == w {
			return r.Fallback
		}
	}
	return ""
}
