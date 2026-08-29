package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/mcp"
)

// exchange runs a session and returns one decoded response per answered call.
func exchange(t *testing.T, s *mcp.Server, calls ...string) []map[string]any {
	t.Helper()
	var out strings.Builder
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(calls, "\n")), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	var got []map[string]any
	dec := json.NewDecoder(strings.NewReader(out.String()))
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("decoding %q: %v", out.String(), err)
		}
		got = append(got, m)
	}
	return got
}

func echoTool() mcp.Tool {
	return mcp.Tool{
		Name: "say", Description: "says something back",
		Call: func(_ context.Context, args map[string]any) (string, error) {
			if w, ok := args["word"].(string); ok {
				return "you said " + w, nil
			}
			return "you said nothing", nil
		},
	}
}

func TestTheHandshakeSpeaksTheVersionTheClientAsksFor(t *testing.T) {
	// Asserting a revision of our own means guessing which one the client
	// speaks, and being wrong fails the handshake for no reason.
	s := mcp.NewServer("msr", "6.0.0", []mcp.Tool{echoTool()})

	got := exchange(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)

	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1", len(got))
	}
	result, _ := got[0]["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want the one asked for", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "msr" {
		t.Errorf("serverInfo = %v", info)
	}
	if _, ok := result["capabilities"].(map[string]any)["tools"]; !ok {
		t.Error("the server should advertise tools")
	}
}

func TestANotificationIsNotAnswered(t *testing.T) {
	// Answering one is a protocol error, and clients differ on how loudly they
	// complain about it.
	s := mcp.NewServer("msr", "6.0.0", []mcp.Tool{echoTool()})

	got := exchange(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	if len(got) != 1 {
		t.Errorf("got %d responses, want only the one for initialize", len(got))
	}
}

func TestToolsAreListedWithWhatAnAgentNeedsToChoose(t *testing.T) {
	s := mcp.NewServer("msr", "6.0.0", []mcp.Tool{echoTool()})

	got := exchange(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)

	tools, _ := got[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("got %d tools", len(tools))
	}
	tool, _ := tools[0].(map[string]any)
	for _, field := range []string{"name", "description", "inputSchema"} {
		if tool[field] == nil {
			t.Errorf("a listed tool needs %q — it is what the agent chooses from", field)
		}
	}
}

func TestCallingAToolReturnsItsTextAsContent(t *testing.T) {
	s := mcp.NewServer("msr", "6.0.0", []mcp.Tool{echoTool()})

	got := exchange(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"say","arguments":{"word":"hello"}}}`)

	result, _ := got[1]["result"].(map[string]any)
	items, _ := result["content"].([]any)
	if len(items) != 1 {
		t.Fatalf("got %+v", result)
	}
	if items[0].(map[string]any)["text"] != "you said hello" {
		t.Errorf("content = %+v", items[0])
	}
	if result["isError"] == true {
		t.Error("a successful call is not an error")
	}
}

func TestAToolThatCannotAnswerSaysSoAsContent(t *testing.T) {
	// An agent can read content. It cannot read a JSON-RPC error code, so a
	// failure the agent might work around should arrive as words.
	s := mcp.NewServer("msr", "6.0.0", []mcp.Tool{{
		Name: "broken", Description: "always fails",
		Call: func(context.Context, map[string]any) (string, error) {
			return "", errNoReview
		},
	}})

	got := exchange(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"broken","arguments":{}}}`)

	result, _ := got[1]["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("a failed tool call should be marked, got %+v", result)
	}
	items, _ := result["content"].([]any)
	if !strings.Contains(items[0].(map[string]any)["text"].(string), "no review") {
		t.Errorf("the failure should say what happened: %+v", items[0])
	}
}

func TestAnUnknownToolIsAProtocolError(t *testing.T) {
	// This one *is* the envelope being wrong: the agent asked for something
	// that does not exist, which is not a result it can act on.
	s := mcp.NewServer("msr", "6.0.0", []mcp.Tool{echoTool()})

	got := exchange(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)

	if got[1]["error"] == nil {
		t.Errorf("want an error for an unknown tool, got %+v", got[1])
	}
}

var errNoReview = errNoReviewType{}

type errNoReviewType struct{}

func (errNoReviewType) Error() string { return "no review is open" }
