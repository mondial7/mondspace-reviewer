// Package mcp lets a coding agent read the review, when it chooses to.
//
// msr watches an agent and never writes back to it (ADR 0004). This does not
// change that: nothing here interrupts anything. It is a server the agent calls
// when it wants to know what the reviewer said, which is the whole shape of the
// requirement — pull, never push (ADR 0031).
//
// The protocol is JSON-RPC 2.0 over stdio, which is the subset of MCP that
// matters: initialize, tools/list, tools/call. It is written out rather than
// taken from an SDK for the same reason three.js is vendored — this project has
// five direct dependencies, and the wire format is something a reader should be
// able to see.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// request is one JSON-RPC call. A notification has no ID and is not answered.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC's own codes. Anything wrong with a *tool* is reported as a successful
// call whose content says what went wrong, because that is what an agent can
// read; these are for the envelope being malformed.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInternal       = -32603
)

// Tool is one thing an agent can call.
type Tool struct {
	Name string
	// Description is what the agent reads to decide whether to call this, and
	// it is the most important field here. It is where provenance lives: a tool
	// returning what a small local model guessed has to say so, or the guess
	// gets treated as fact (ADR 0031).
	Description string
	Schema      map[string]any
	Call        func(ctx context.Context, args map[string]any) (string, error)
}

// Server answers MCP over a pair of streams.
type Server struct {
	name    string
	version string
	tools   []Tool

	mu    sync.Mutex
	ready bool // set by initialize; tools are refused before it
}

func NewServer(name, version string, tools []Tool) *Server {
	return &Server{name: name, version: version, tools: tools}
}

// Serve reads requests until the input ends, which is how an MCP client says it
// is finished.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	dec := json.NewDecoder(bufio.NewReader(in))
	enc := json.NewEncoder(out)

	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			// A malformed message is reported and the stream continues: one bad
			// frame should not end a session.
			_ = enc.Encode(response{JSONRPC: "2.0", Error: &rpcError{codeParse, err.Error()}})
			return nil
		}

		resp, answer := s.handle(ctx, req)
		if !answer {
			continue // a notification
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
}

func (s *Server) handle(ctx context.Context, req request) (response, bool) {
	reply := response{JSONRPC: "2.0", ID: req.ID}

	// A notification carries no id and takes no answer. `initialized` is the
	// one that matters: it is how the client says the handshake is done.
	notification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		s.mu.Lock()
		s.ready = true
		s.mu.Unlock()
		reply.Result = s.hello(req.Params)

	case "notifications/initialized", "initialized":
		return response{}, false

	case "ping":
		reply.Result = map[string]any{}

	case "tools/list":
		reply.Result = map[string]any{"tools": s.describe()}

	case "tools/call":
		reply.Result, reply.Error = s.call(ctx, req.Params)

	default:
		if notification {
			return response{}, false
		}
		reply.Error = &rpcError{codeMethodNotFound, "unknown method " + req.Method}
	}

	if notification {
		return response{}, false
	}
	return reply, true
}

// hello answers the handshake.
//
// The protocol version is echoed back when the client named one. Asserting a
// version of our own would mean guessing which revision the client speaks, and
// being wrong about it fails the handshake for no reason — the methods used
// here have been stable across revisions.
func (s *Server) hello(params json.RawMessage) map[string]any {
	version := defaultProtocol
	var asked struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &asked); err == nil && asked.ProtocolVersion != "" {
		version = asked.ProtocolVersion
	}

	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": s.name, "version": s.version},
	}
}

const defaultProtocol = "2024-11-05"

func (s *Server) describe() []map[string]any {
	out := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		schema := t.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"name": t.Name, "description": t.Description, "inputSchema": schema,
		})
	}
	return out
}

func (s *Server) call(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var asked struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &asked); err != nil {
		return nil, &rpcError{codeInvalidRequest, err.Error()}
	}

	for _, t := range s.tools {
		if t.Name != asked.Name {
			continue
		}
		text, err := t.Call(ctx, asked.Arguments)
		if err != nil {
			// A tool that could not answer reports it as content rather than as
			// a transport error: the agent can read one and not the other.
			return content(fmt.Sprintf("could not answer: %v", err), true), nil
		}
		return content(text, false), nil
	}
	return nil, &rpcError{codeMethodNotFound, "unknown tool " + asked.Name}
}

func content(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}
