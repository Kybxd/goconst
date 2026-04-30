package analyzer_test

import (
	"testing"

	"github.com/Kybxd/goconst/cmd/nilcompare/analyzer"
	"golang.org/x/tools/go/analysis/analysistest"
)

// TestAnalyzer runs the analyzer against testdata/src/a under the GOPATH
// loader mode (no go.mod anywhere in testdata/), so the sample code
// imports a minimal stub at testdata/src/goconst rather than the real
// github.com/Kybxd/goconst runtime package. Repointing
// analyzer.DoNotComparePkgPath at the stub import path is what makes
// this possible without a replace directive.
//
// The `// want` regex comments in testdata/src/a/a.go are the source of
// truth for expected diagnostics; analysistest.Run fails the test if
// expected ones are missing or extra ones appear.
func TestAnalyzer(t *testing.T) {
	prev := analyzer.DoNotComparePkgPath
	analyzer.DoNotComparePkgPath = "goconst"
	t.Cleanup(func() { analyzer.DoNotComparePkgPath = prev })

	analysistest.Run(t, analysistest.TestData(), analyzer.Analyzer, "a")
}

// TestSuggestedFixes applies the analyzer's auto-fixes to
// testdata/src/a/a.go and compares the result against the accompanying
// a.go.golden golden file. Diagnostics without a SuggestedFix (the
// switch-case ones) leave their source unchanged, so the golden still
// contains `switch d { case nil: ... }`.
func TestSuggestedFixes(t *testing.T) {
	prev := analyzer.DoNotComparePkgPath
	analyzer.DoNotComparePkgPath = "goconst"
	t.Cleanup(func() { analyzer.DoNotComparePkgPath = prev })

	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), analyzer.Analyzer, "a")
}
