// Package goconst is a minimal stand-in for github.com/Kybxd/goconst
// used by the nilcompare analyzer's testdata. The analyzer only looks
// up the marker interface by (package path, type name), so reproducing
// the shape of the real runtime type is enough.
package goconst

// DoNotCompare mirrors the method set of the real goconst.DoNotCompare.
type DoNotCompare interface {
	IsNil() bool
}
