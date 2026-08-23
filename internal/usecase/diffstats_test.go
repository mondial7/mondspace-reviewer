package usecase_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

func TestDiffHeadline(t *testing.T) {
	added := domain.Diff{Text: "diff --git a/x.go b/x.go\nnew file mode 100644\n+package x\n"}
	if h := usecase.DiffHeadline("pkg/x.go", added); h.Text != "added x.go" || h.WhySrc != domain.WhyInferred {
		t.Errorf("new file headline = %+v, want 'added x.go' inferred", h)
	}
	deleted := domain.Diff{Text: "diff --git a/x.go b/x.go\ndeleted file mode 100644\n-package x\n"}
	if h := usecase.DiffHeadline("pkg/x.go", deleted); h.Text != "removed x.go" {
		t.Errorf("deleted headline = %q, want 'removed x.go'", h.Text)
	}
	modified := domain.Diff{Text: "@@ -1 +1 @@\n-a\n+b\n"}
	if h := usecase.DiffHeadline("pkg/x.go", modified); h.Text != "edited x.go" {
		t.Errorf("modified headline = %q, want 'edited x.go'", h.Text)
	}
}

func TestDiffStats(t *testing.T) {
	d := domain.Diff{Text: "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,2 +1,3 @@\n context\n-removed one\n+added one\n+added two\n"}

	added, removed := usecase.DiffStats(d)

	if added != 2 || removed != 1 {
		t.Errorf("DiffStats = +%d -%d, want +2 -1 (headers and context excluded)", added, removed)
	}
	if a, r := usecase.DiffStats(domain.Diff{}); a != 0 || r != 0 {
		t.Errorf("empty diff = +%d -%d, want +0 -0", a, r)
	}
}
