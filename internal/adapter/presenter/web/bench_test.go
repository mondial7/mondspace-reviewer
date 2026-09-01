package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/presenter/web"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// bigSession is a review of n files, each with a diff of a realistic size.
func bigSession(n int) web.Session {
	sess := web.Session{ID: "big", Prompt: "a large range", Diffs: map[string]domain.Diff{}}
	var diff strings.Builder
	diff.WriteString("@@ -1,4 +1,34 @@\n package x\n")
	for i := 0; i < 30; i++ {
		diff.WriteString(fmt.Sprintf("+\t// line %d\n", i))
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("big-f%03d", i)
		sess.Units = append(sess.Units, domain.Unit{
			ID: id, SessionID: "big",
			Files:    []string{fmt.Sprintf("pkg%d/file%d.go", i/50, i)},
			Headline: domain.Headline{Text: "changed"},
		})
		sess.Diffs[id] = domain.Diff{Text: diff.String()}
	}
	return sess
}

// BenchmarkCockpitWired renders with everything attached, which is how it is
// actually used. The bare benchmark below missed a twenty-eight second page
// because none of the wiring was there to be slow (ADR 0029).
func BenchmarkCockpitWired(b *testing.B) {
	for _, n := range []int{200, 600} {
		b.Run(fmt.Sprintf("files=%d", n), func(b *testing.B) {
			sess := bigSession(n)
			h := web.NewServer(sess, nil).
				WithStats(domain.SessionStats{Files: n}).
				WithAnalyses(
					func(context.Context, string, domain.AnalysisKind) error { return nil },
					func(string, domain.AnalysisKind, string) domain.Analysis { return domain.Analysis{} }).
				WithSignoff(
					func(context.Context, domain.Signoff) error { return nil },
					func(string) domain.Signoff { return domain.Signoff{} }).
				WithAsk(func(context.Context, string, string, []web.Exchange) (string, error) {
					return "", nil
				})
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d", rec.Code)
				}
			}
		})
	}
}

func BenchmarkCockpitRender(b *testing.B) {
	for _, n := range []int{50, 200, 600} {
		b.Run(fmt.Sprintf("files=%d", n), func(b *testing.B) {
			h := web.NewServer(bigSession(n), nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d", rec.Code)
				}
			}
		})
	}
}
