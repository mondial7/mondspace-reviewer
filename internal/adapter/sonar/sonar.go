// Package sonar pulls the issues a SonarQube or SonarCloud server has already
// computed for a project (ADR 0043).
//
// A pull, never a scan. The scanner has no local-only mode: it is a client
// reporting to a server that stores and processes results, which is a JVM and a
// server process beside a 4 GB model, and it runs at CI cadence rather than
// inside a five-second poll. What is worth having is the answer a team's server
// already has, and that is one HTTP request.
//
// Everything about this adapter is therefore different from the others: it is
// the only one that needs the network, and the only one configured by
// environment rather than by `.msr.toml` — a token does not belong in a file
// that is checked in.
package sonar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// Tool is what findings from this adapter are attributed to.
const Tool = "sonar"

// pageSize is what one request asks for. Sonar's own maximum is 500.
const pageSize = 500

// maxPages bounds a pull. A project with more than this many open issues is one
// nobody is going to read the tail of, and an unbounded loop against somebody
// else's server is not something a review tool should start.
const maxPages = 5

// Client is a Sonar server and the project to ask it about.
type Client struct {
	BaseURL string
	Project string
	Token   string
	HTTP    *http.Client
}

// FromEnv builds a client from the environment, and reports whether one is
// configured at all.
//
// Not configured is the normal case and is silent: a reviewer who has never
// heard of Sonar should see no mention of it anywhere.
func FromEnv() (*Client, bool) {
	base, project := os.Getenv("MSR_SONAR_URL"), os.Getenv("MSR_SONAR_PROJECT")
	if base == "" || project == "" {
		return nil, false
	}
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		Project: project,
		Token:   os.Getenv("MSR_SONAR_TOKEN"),
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}, true
}

// issuesResponse is the part of `api/issues/search` this reads.
type issuesResponse struct {
	Total  int `json:"total"`
	Issues []struct {
		Rule      string `json:"rule"`
		Severity  string `json:"severity"`
		Component string `json:"component"`
		Line      int    `json:"line"`
		Message   string `json:"message"`
		Impacts   []struct {
			Severity string `json:"severity"`
		} `json:"impacts"`
	} `json:"issues"`
}

// Issues is every unresolved issue the server holds for this project, as
// findings attributed to `sonar`.
func (c *Client) Issues(ctx context.Context) ([]domain.Reported, error) {
	var out []domain.Reported
	for page := 1; page <= maxPages; page++ {
		body, err := c.page(ctx, page)
		if err != nil {
			return nil, err
		}
		for _, issue := range body.Issues {
			out = append(out, domain.Reported{
				Tool:     Tool,
				Rule:     issue.Rule,
				File:     path(issue.Component, c.Project),
				Line:     issue.Line,
				Message:  issue.Message,
				Severity: severity(issue.Severity, impact(issue.Impacts)),
			})
		}
		if len(out) >= body.Total || len(body.Issues) == 0 {
			break
		}
	}
	return out, nil
}

func (c *Client) page(ctx context.Context, page int) (issuesResponse, error) {
	query := url.Values{
		"componentKeys": {c.Project},
		"resolved":      {"false"},
		"ps":            {strconv.Itoa(pageSize)},
		"p":             {strconv.Itoa(page)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/issues/search?"+query.Encode(), nil)
	if err != nil {
		return issuesResponse{}, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return issuesResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return issuesResponse{}, fmt.Errorf("sonar: %s said %s", c.BaseURL, resp.Status)
	}

	var body issuesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return issuesResponse{}, fmt.Errorf("sonar: reading the response: %w", err)
	}
	return body, nil
}

// path turns a component key back into a repository-relative path.
//
// Sonar names a file `<project>:<path>`, and on a project analysed as several
// modules there can be more than one colon. Only the first is the project.
func path(component, project string) string {
	if trimmed := strings.TrimPrefix(component, project+":"); trimmed != component {
		return trimmed
	}
	if _, rest, found := strings.Cut(component, ":"); found {
		return rest
	}
	return component
}

// impact is the severity from Sonar's newer clean-code model, which reports a
// list of impacts rather than one level. The worst of them is the one that
// matters.
func impact(impacts []struct {
	Severity string `json:"severity"`
}) string {
	worst := ""
	for _, i := range impacts {
		if rank(i.Severity) < rank(worst) {
			worst = i.Severity
		}
	}
	return worst
}

// severity maps Sonar's vocabulary onto msr's three levels, preferring the
// clean-code impact when the server sent one.
func severity(legacy, impact string) domain.Severity {
	word := impact
	if word == "" {
		word = legacy
	}
	switch strings.ToUpper(word) {
	case "BLOCKER", "CRITICAL", "HIGH":
		return domain.SeverityHigh
	case "MINOR", "INFO", "LOW":
		return domain.SeverityLow
	default:
		return domain.SeverityMedium
	}
}

func rank(word string) int {
	switch strings.ToUpper(word) {
	case "BLOCKER", "CRITICAL", "HIGH":
		return 0
	case "MAJOR", "MEDIUM":
		return 1
	case "MINOR", "INFO", "LOW":
		return 2
	default:
		return 3
	}
}
