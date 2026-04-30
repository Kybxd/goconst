// Command nilcompare is a standalone go vet-compatible driver for the
// nilcompare analyzer. Install it with
//
//	go install github.com/Kybxd/goconst/cmd/nilcompare@latest
//
// and either run it directly against your module
//
//	nilcompare ./...
//
// or wire it as a vet tool
//
//	go vet -vettool=$(which nilcompare) ./...
//
// The analyzer rejects comparisons of the untyped nil literal to any
// interface value whose method set embeds goconst.DoNotCompare — i.e.
// any generated Message_Const view and any goconst.Slice / goconst.Map.
// See the analyzer subpackage for rationale and examples.
package main

import (
	"github.com/Kybxd/goconst/cmd/nilcompare/analyzer"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(analyzer.Analyzer) }
