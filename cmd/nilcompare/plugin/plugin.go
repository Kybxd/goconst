// Package plugin exposes the nilcompare analyzer as a golangci-lint v2
// custom plugin. See https://golangci-lint.run/plugins/module-plugins/
// for the module-plugin-system setup; .custom-gcl.yml / .golangci.yml
// wiring looks roughly like:
//
//	# .custom-gcl.yml
//	version: v1.62.0
//	plugins:
//	  - module: 'github.com/Kybxd/goconst'
//	    import: 'github.com/Kybxd/goconst/cmd/nilcompare/plugin'
//	    version: 'v0.3.0'   # or a pseudo-version / local replace
//
//	# .golangci.yml
//	linters-settings:
//	  custom:
//	    nilcompare:
//	      type: module
//	      description: forbid comparing DoNotCompare-bearing interfaces to nil
//	  linters:
//	    enable:
//	      - nilcompare
//
// The plugin has no user-tunable settings; the New entrypoint just
// ignores its argument and returns a LinterPlugin that exposes our
// single analyzer.
package plugin

import (
	"github.com/Kybxd/goconst/cmd/nilcompare/analyzer"
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("nilcompare", New)
}

// New is the golangci-lint custom-plugin entrypoint. The settings
// argument is currently unused.
func New(settings any) (register.LinterPlugin, error) {
	return &plugin{}, nil
}

type plugin struct{}

// BuildAnalyzers returns the analyzer list this plugin contributes.
func (*plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{analyzer.Analyzer}, nil
}

// GetLoadMode controls how golangci-lint feeds packages to the
// analyzer. LoadModeTypesInfo is required because operandTypeEmbedsMarker
// reads pass.TypesInfo.Types.
func (*plugin) GetLoadMode() string { return register.LoadModeTypesInfo }
