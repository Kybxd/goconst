package main

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// graphOf builds a mapValueGraph from a literal adjacency list so the
// cycle criterion can be tested without a CodeGeneratorRequest.
func graphOf(adj map[string][]string) *mapValueGraph {
	g := &mapValueGraph{edges: make(map[protoreflect.FullName]map[protoreflect.FullName]bool)}
	for owner, values := range adj {
		for _, v := range values {
			g.addEdge(protoreflect.FullName(owner), protoreflect.FullName(v))
		}
	}
	return g
}

func TestMapValueGraphOnCycle(t *testing.T) {
	tests := []struct {
		name  string
		adj   map[string][]string
		owner string
		value string
		want  bool
	}{{
		name:  "self edge",
		adj:   map[string][]string{"A": {"A"}},
		owner: "A", value: "A", want: true,
	}, {
		name:  "two-node cycle, first edge",
		adj:   map[string][]string{"A": {"B"}, "B": {"A"}},
		owner: "A", value: "B", want: true,
	}, {
		name:  "two-node cycle, second edge",
		adj:   map[string][]string{"A": {"B"}, "B": {"A"}},
		owner: "B", value: "A", want: true,
	}, {
		name:  "three-node cycle",
		adj:   map[string][]string{"A": {"B"}, "B": {"C"}, "C": {"A"}},
		owner: "B", value: "C", want: true,
	}, {
		name:  "plain chain is acyclic",
		adj:   map[string][]string{"A": {"B"}, "B": {"C"}},
		owner: "A", value: "B", want: false,
	}, {
		// The precision case: A points at B, which sits on its own
		// cycle, but B cannot reach A.
		name:  "edge into a cycle is not on the cycle",
		adj:   map[string][]string{"A": {"B"}, "B": {"B"}},
		owner: "A", value: "B", want: false,
	}, {
		name:  "edge into a two-node cycle is not on the cycle",
		adj:   map[string][]string{"A": {"B"}, "B": {"C"}, "C": {"B"}},
		owner: "A", value: "B", want: false,
	}, {
		name:  "diamond without a back edge",
		adj:   map[string][]string{"A": {"B", "C"}, "B": {"D"}, "C": {"D"}},
		owner: "A", value: "B", want: false,
	}, {
		name:  "long cycle reached through a branch",
		adj:   map[string][]string{"A": {"B"}, "B": {"C", "X"}, "C": {"D"}, "D": {"A"}},
		owner: "A", value: "B", want: true,
	}, {
		name:  "no edges at all",
		adj:   map[string][]string{},
		owner: "A", value: "B", want: false,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := graphOf(tt.adj).onCycle(
				protoreflect.FullName(tt.owner),
				protoreflect.FullName(tt.value),
			)
			if got != tt.want {
				t.Fatalf("onCycle(%s, %s) = %v, want %v",
					tt.owner, tt.value, got, tt.want)
			}
		})
	}
}

// TestMapValueGraphTerminatesOnCycle guards against the traversal
// itself looping forever on a cyclic graph when the answer is "no".
func TestMapValueGraphTerminatesOnCycle(t *testing.T) {
	g := graphOf(map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"B"}, // cycle not involving the query's owner
	})
	if g.onCycle("Z", "A") {
		t.Fatal("unrelated owner must not be reported on a cycle")
	}
}

func TestBuildExcludePatterns(t *testing.T) {
	got := buildExcludePatterns([]string{" a/b ", "", "   ", "c/**"})
	want := []string{"a/b", "c/**", builtinExcludePackagePatterns[0]}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestMatchExcludePattern(t *testing.T) {
	patterns := buildExcludePatterns([]string{"example.com/a", "example.com/vendor/**"})
	cases := map[string]bool{
		"example.com/a":                                true,
		"example.com/ab":                               false,
		"example.com/vendor/x":                         true,
		"example.com/vendor/x/y/z":                     true,
		"example.com/other":                            false,
		"google.golang.org/protobuf/types/known/anypb": true, // built-in default
	}
	for path, want := range cases {
		if got := matchExcludePattern(patterns, path); got != want {
			t.Errorf("matchExcludePattern(%q) = %v, want %v", path, got, want)
		}
	}
}
