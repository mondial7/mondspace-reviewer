package usecase_test

import (
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func hasFlag(flags []domain.Flag, want domain.Flag) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// diffWithChangedLines builds a unified-diff body with n added and n removed
// content lines, plus file headers that must not be counted.
func diffWithChangedLines(n int) domain.Diff {
	var b strings.Builder
	b.WriteString("diff --git a/x.go b/x.go\n")
	b.WriteString("--- a/x.go\n")
	b.WriteString("+++ b/x.go\n")
	b.WriteString("@@ -1,1 +1,1 @@\n")
	for i := 0; i < n; i++ {
		b.WriteString("-old line\n")
		b.WriteString("+new line\n")
	}
	return domain.Diff{Text: b.String(), Files: []string{"x.go"}}
}

func TestFlagLarge(t *testing.T) {
	// 76 added + 76 removed = 152 changed lines, over the 150 threshold.
	if !hasFlag(usecase.Flags(domain.Unit{}, diffWithChangedLines(76)), domain.FlagLarge) {
		t.Error("expected large flag for 152 changed lines")
	}
	// 75 + 75 = 150, not over.
	if hasFlag(usecase.Flags(domain.Unit{}, diffWithChangedLines(75)), domain.FlagLarge) {
		t.Error("did not expect large flag for exactly 150 changed lines")
	}
}

func TestFlagTodo(t *testing.T) {
	added := domain.Diff{Text: "@@ -1 +1 @@\n+\t// TODO: handle the edge case\n"}
	if !hasFlag(usecase.Flags(domain.Unit{}, added), domain.FlagTodo) {
		t.Error("expected todo flag for an added TODO line")
	}

	for _, kind := range []string{"FIXME", "XXX"} {
		d := domain.Diff{Text: "+// " + kind + " later\n"}
		if !hasFlag(usecase.Flags(domain.Unit{}, d), domain.FlagTodo) {
			t.Errorf("expected todo flag for added %s", kind)
		}
	}

	// A TODO that is being removed, or sitting in unchanged context, is not new.
	removed := domain.Diff{Text: "@@ -1 +1 @@\n-// TODO: old note\n context TODO stays\n"}
	if hasFlag(usecase.Flags(domain.Unit{}, removed), domain.FlagTodo) {
		t.Error("did not expect todo flag for a removed/context TODO")
	}
}

func TestFlagNewDepOnlyOnFilesThatManageDependencies(t *testing.T) {
	// The flag means "this change takes on a dependency", which is a supply-chain
	// question. It used to fire on any added `import` line, so every Go file that
	// gained an internal import was flagged — and a flag that fires constantly is
	// one a reviewer learns to ignore.
	code := domain.Unit{Files: []string{"internal/tui/open.go"}}
	imports := domain.Diff{Text: "@@\n+import (\n+\t\"fmt\"\n+\t\"github.com/charmbracelet/lipgloss\"\n+)\n"}

	if hasFlag(usecase.Flags(code, imports), domain.FlagNewDep) {
		t.Error("importing something in a source file is not taking on a dependency")
	}
}

func TestFlagNewDepOnManifests(t *testing.T) {
	cases := []struct {
		file string
		line string
	}{
		{"go.mod", "+\tgithub.com/foo/bar v1.0.0"},
		{"go.mod", "+require github.com/x/y v1.2.3"},
		{"package.json", `+    "lodash": "^4.17.21",`},
		{"web/package.json", `+    "react": "18.2.0",`},
		{"Cargo.toml", `+serde = "1.0"`},
		{"requirements.txt", "+requests==2.31.0"},
		{"Gemfile", `+gem "rails", "~> 7.0"`},
		{"pyproject.toml", `+httpx = "^0.27"`},
	}
	for _, c := range cases {
		u := domain.Unit{Files: []string{c.file}}
		if !hasFlag(usecase.Flags(u, domain.Diff{Text: c.line + "\n"}), domain.FlagNewDep) {
			t.Errorf("expected new-dep for %s adding %q", c.file, c.line)
		}
	}
}

func TestFlagNewDepOnLockFilesForAnyAddition(t *testing.T) {
	// A lock file exists only to pin dependencies, so anything added to one is a
	// dependency change — its lines do not look like the manifest's.
	for _, f := range []string{"go.sum", "package-lock.json", "yarn.lock", "Cargo.lock", "pnpm-lock.yaml"} {
		u := domain.Unit{Files: []string{f}}
		diff := domain.Diff{Text: "@@\n+  resolved \"https://registry.example/x\"\n"}
		if !hasFlag(usecase.Flags(u, diff), domain.FlagNewDep) {
			t.Errorf("expected new-dep for an addition to %s", f)
		}
	}
}

func TestFlagNewDepStillCatchesARealDependency(t *testing.T) {
	// The real diff from the commit that promoted pgx to a direct dependency.
	// Narrowing the rule must not quietly turn the flag off — that would be
	// worse than the noise it was fixing.
	u := domain.Unit{Files: []string{"go.mod"}}
	d := domain.Diff{Text: "@@ -5,7 +5,9 @@ go 1.25\n require (\n \tgithub.com/charmbracelet/bubbletea v1.3.10\n+\tgithub.com/jackc/pgx/v5 v5.7.6\n \tgithub.com/oklog/ulid/v2 v2.1.1\n )\n"}

	if !hasFlag(usecase.Flags(u, d), domain.FlagNewDep) {
		t.Error("a genuinely added dependency in go.mod must still be flagged")
	}
}

func TestFlagNewDepIgnoresNonDependencyChangesToAManifest(t *testing.T) {
	// Bumping the language version in go.mod is not taking on a dependency, and
	// neither is removing one.
	u := domain.Unit{Files: []string{"go.mod"}}

	for _, line := range []string{"+go 1.25", "+module github.com/x/y", "-\tgithub.com/old/dep v1.0.0"} {
		if hasFlag(usecase.Flags(u, domain.Diff{Text: line + "\n"}), domain.FlagNewDep) {
			t.Errorf("did not expect new-dep for %q in go.mod", line)
		}
	}
}

func TestFlagSwallowedErr(t *testing.T) {
	swallows := []string{
		"+\t_ = doThing()",
		"+\t_ = f.Close()",
	}
	for _, line := range swallows {
		if !hasFlag(usecase.Flags(domain.Unit{}, domain.Diff{Text: line + "\n"}), domain.FlagSwallowedErr) {
			t.Errorf("expected swallowed-err for %q", line)
		}
	}

	clean := []string{
		"+\t_, err := f.Read(b)",
		"+\tx = compute()",
		"+\t_ = someVar",
	}
	for _, line := range clean {
		if hasFlag(usecase.Flags(domain.Unit{}, domain.Diff{Text: line + "\n"}), domain.FlagSwallowedErr) {
			t.Errorf("did not expect swallowed-err for %q", line)
		}
	}
}

func TestFlagsOrderedAndCleanUnit(t *testing.T) {
	// A unit that trips no-test, todo, new-dep and public-api at once. new-dep
	// needs a manifest in the unit now: an import in a source file is not taking
	// on a dependency.
	u := domain.Unit{Files: []string{"api.go", "go.mod"}}
	d := domain.Diff{Text: strings.Join([]string{
		`-func OldAPI() {`,
		`+	github.com/x/y v1.2.3`,
		`+// TODO: revisit`,
	}, "\n")}

	got := usecase.Flags(u, d)
	want := []domain.Flag{domain.FlagNoTest, domain.FlagTodo, domain.FlagNewDep, domain.FlagPublicAPI}
	if len(got) != len(want) {
		t.Fatalf("flags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("flags = %v, want %v (order matters)", got, want)
		}
	}

	if flags := usecase.Flags(domain.Unit{Files: []string{"a.go", "a_test.go"}}, domain.Diff{}); len(flags) != 0 {
		t.Errorf("clean unit flags = %v, want none", flags)
	}
}

func TestFlagsOrderedWithSoloIfaceLast(t *testing.T) {
	// solo-iface is deterministic-order's newest member: it comes after
	// public-api, alongside no-test/todo/new-dep tripped by the same diff.
	u := domain.Unit{Files: []string{"api.go", "go.mod"}}
	d := domain.Diff{Text: strings.Join([]string{
		`+	github.com/x/y v1.2.3`,
		`+// TODO: revisit`,
		`+type Validator interface {`,
		`+	Validate(token string) error`,
		`+}`,
	}, "\n")}

	got := usecase.Flags(u, d)
	want := []domain.Flag{domain.FlagNoTest, domain.FlagTodo, domain.FlagNewDep, domain.FlagSoloIface}
	if len(got) != len(want) {
		t.Fatalf("flags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("flags = %v, want %v (order matters)", got, want)
		}
	}
}

func TestFlagPublicAPI(t *testing.T) {
	changed := []string{
		"-func Foo() {",
		"-type Config struct {",
		"-func (s *Server) Start() error {",
		"-var DefaultTimeout = 5",
		"-const MaxSize = 10",
	}
	for _, line := range changed {
		if !hasFlag(usecase.Flags(domain.Unit{}, domain.Diff{Text: line + "\n"}), domain.FlagPublicAPI) {
			t.Errorf("expected public-api for removed %q", line)
		}
	}

	clean := []string{
		"-func helper() {",           // unexported
		"+func NewThing() {",         // added, not removed/changed
		"-\tx := 1",                  // not a declaration
		"-type internalState struct", // unexported
	}
	for _, line := range clean {
		if hasFlag(usecase.Flags(domain.Unit{}, domain.Diff{Text: line + "\n"}), domain.FlagPublicAPI) {
			t.Errorf("did not expect public-api for %q", line)
		}
	}
}

func TestFlagSoloIface(t *testing.T) {
	newIfaceNoImpl := domain.Diff{Text: strings.Join([]string{
		`+type Validator interface {`,
		`+	Validate(token string) error`,
		`+}`,
	}, "\n")}
	if !hasFlag(usecase.Flags(domain.Unit{}, newIfaceNoImpl), domain.FlagSoloIface) {
		t.Error("expected solo-iface for a new interface with no implementing method in the diff")
	}

	newIfaceWithImpl := domain.Diff{Text: strings.Join([]string{
		`+type Validator interface {`,
		`+	Validate(token string) error`,
		`+}`,
		`+`,
		`+func (v *tokenValidator) Validate(token string) error {`,
		`+	return nil`,
		`+}`,
	}, "\n")}
	if hasFlag(usecase.Flags(domain.Unit{}, newIfaceWithImpl), domain.FlagSoloIface) {
		t.Error("did not expect solo-iface when the diff also adds a matching method")
	}

	noIface := domain.Diff{Text: strings.Join([]string{
		`+func (v *tokenValidator) Validate(token string) error {`,
		`+	return nil`,
		`+}`,
	}, "\n")}
	if hasFlag(usecase.Flags(domain.Unit{}, noIface), domain.FlagSoloIface) {
		t.Error("did not expect solo-iface with no new interface in the diff")
	}

	// A removed interface is not "new" — nothing to flag.
	removedIface := domain.Diff{Text: strings.Join([]string{
		`-type Validator interface {`,
		`-	Validate(token string) error`,
		`-}`,
	}, "\n")}
	if hasFlag(usecase.Flags(domain.Unit{}, removedIface), domain.FlagSoloIface) {
		t.Error("did not expect solo-iface for a removed interface")
	}
}

func TestFlagNoTest(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  bool
	}{
		{"non-test source, no test", []string{"auth/token.go"}, true},
		{"source with a test file", []string{"auth/token.go", "auth/token_test.go"}, false},
		{"only a test file", []string{"auth/token_test.go"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := domain.Unit{Files: tt.files}
			got := hasFlag(usecase.Flags(u, domain.Diff{}), domain.FlagNoTest)
			if got != tt.want {
				t.Errorf("no-test = %v, want %v", got, tt.want)
			}
		})
	}
}
