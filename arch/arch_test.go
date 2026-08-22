package arch

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const forbiddenPrefix = "github.com/mondial7/mondspace-reviewer/internal/adapter/"

// innerPackages depend only inward; an adapter import from any of them
// inverts the dependency direction the whole design rests on.
var innerPackages = []string{"domain", "usecase", "port"}

func TestInnerPackagesDoNotImportAdapters(t *testing.T) {
	for _, pkg := range innerPackages {
		t.Run(pkg, func(t *testing.T) {
			root := filepath.Join("..", "internal", pkg)
			if _, err := os.Stat(root); os.IsNotExist(err) {
				t.Skipf("%s does not exist yet", root)
			}
			for _, file := range goFiles(t, root) {
				for _, imp := range adapterImports(t, file) {
					t.Errorf("%s imports adapter package %s", file, imp)
				}
			}
		})
	}
}

func TestAdapterImportIsDetected(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bad.go")
	source, err := os.ReadFile(filepath.Join("testdata", "violation", "bad.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, source, 0o600); err != nil {
		t.Fatal(err)
	}

	got := adapterImports(t, file)

	want := []string{forbiddenPrefix + "store/jsonl"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("adapterImports = %v, want %v", got, want)
	}
}

func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(found) == 0 {
		t.Fatalf("no go files found under %s", root)
	}
	return found
}

func adapterImports(t *testing.T, file string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	var forbidden []string
	for _, spec := range parsed.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("bad import path in %s: %v", file, err)
		}
		if strings.HasPrefix(path, forbiddenPrefix) {
			forbidden = append(forbidden, path)
		}
	}
	return forbidden
}
