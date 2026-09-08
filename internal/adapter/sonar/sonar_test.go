package sonar_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/sonar"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

func server(t *testing.T, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("resolved"); got != "false" {
			t.Errorf("resolved=%q, want false — a closed issue is not a finding", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestIssuesBecomeFindings(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"total": 2,
		"issues": []map[string]any{
			{"rule": "go:S2245", "severity": "CRITICAL", "component": "proj:internal/a.go", "line": 12, "message": "weak random"},
			{"rule": "go:S1234", "severity": "MINOR", "component": "proj:internal/b.go", "line": 3, "message": "small"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := server(t, string(body))
	client := &sonar.Client{BaseURL: s.URL, Project: "proj", Token: "tok", HTTP: s.Client()}

	got, err := client.Issues(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("pulled %d issues, want 2", len(got))
	}
	if got[0].Tool != sonar.Tool || got[0].Rule != "go:S2245" {
		t.Errorf("attribution = %q/%q", got[0].Tool, got[0].Rule)
	}
	if got[0].File != "internal/a.go" {
		t.Errorf("file = %q, want the project key stripped", got[0].File)
	}
	if got[0].Severity != domain.SeverityHigh || got[1].Severity != domain.SeverityLow {
		t.Errorf("severities = %q and %q", got[0].Severity, got[1].Severity)
	}
}

// Newer servers report clean-code impacts rather than one level, and the worst
// impact is the one that matters.
func TestImpactsWinOverTheLegacySeverity(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"total": 1,
		"issues": []map[string]any{{
			"rule": "go:S1", "severity": "MINOR", "component": "proj:a.go", "line": 1, "message": "x",
			"impacts": []map[string]any{{"severity": "LOW"}, {"severity": "HIGH"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := server(t, string(body))
	client := &sonar.Client{BaseURL: s.URL, Project: "proj", Token: "tok", HTTP: s.Client()}

	got, err := client.Issues(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if got[0].Severity != domain.SeverityHigh {
		t.Errorf("severity = %q, want the worst impact", got[0].Severity)
	}
}

// A server that says no is an error the caller can report, not a panic and not
// a silent empty answer that would look like "nothing found".
func TestARefusalIsAnError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer s.Close()
	client := &sonar.Client{BaseURL: s.URL, Project: "proj", HTTP: s.Client()}

	if _, err := client.Issues(context.Background()); err == nil {
		t.Error("401 came back as success")
	}
}

// A reviewer who has never heard of Sonar sees no mention of it.
func TestNotConfiguredIsSilent(t *testing.T) {
	t.Setenv("MSR_SONAR_URL", "")
	t.Setenv("MSR_SONAR_PROJECT", "")

	if _, configured := sonar.FromEnv(); configured {
		t.Error("an unconfigured environment produced a client")
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("MSR_SONAR_URL", "https://sonar.example.com/")
	t.Setenv("MSR_SONAR_PROJECT", "proj")
	t.Setenv("MSR_SONAR_TOKEN", "tok")

	client, configured := sonar.FromEnv()

	if !configured {
		t.Fatal("a configured environment produced no client")
	}
	if client.BaseURL != "https://sonar.example.com" {
		t.Errorf("BaseURL = %q, want the trailing slash gone", client.BaseURL)
	}
}
