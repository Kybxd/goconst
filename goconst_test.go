package goconst

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// -----------------------------------------------------------------
// Slice.Clone — header independence + shallow per-element copy.
// -----------------------------------------------------------------

func TestSlice_Clone_HeaderIndependent(t *testing.T) {
	src := []string{"a", "b", "c"}
	view := NewSlice(src)

	got := view.Clone()
	if !reflect.DeepEqual(got, src) {
		t.Fatalf("Clone content = %v, want %v", got, src)
	}

	// Mutating the clone must not reach back into the view.
	got[0] = "X"
	if view.At(0) != "a" {
		t.Fatalf("view.At(0) = %q after mutating clone, want %q", view.At(0), "a")
	}
	// And mutating the original backing slice must not show up in
	// the (already-taken) clone.
	src[1] = "Y"
	if got[1] != "b" {
		t.Fatalf("clone[1] = %q after mutating src, want %q", got[1], "b")
	}
}

func TestSlice_Clone_NilSlice(t *testing.T) {
	view := NewSlice[string](nil)
	got := view.Clone()
	if got != nil {
		t.Fatalf("Clone of nil-backed Slice = %v, want nil", got)
	}
}

// -----------------------------------------------------------------
// Slice.IsNil / Map.IsNil — strictly report whether the underlying
// slice / map header is nil. An empty but non-nil header reports
// false (the field is "present but empty", which is distinct from
// "absent"); use Len() == 0 for the "nothing to read" reading.
// -----------------------------------------------------------------

func TestSlice_IsNil(t *testing.T) {
	if got := NewSlice[string](nil).IsNil(); !got {
		t.Errorf("nil-backed Slice.IsNil() = false, want true")
	}
	if got := NewSlice([]string{}).IsNil(); got {
		t.Errorf("empty-but-non-nil Slice.IsNil() = true, want false")
	}
	if got := NewSlice([]string{"a"}).IsNil(); got {
		t.Errorf("populated Slice.IsNil() = true, want false")
	}
}

func TestMap_IsNil(t *testing.T) {
	if got := NewMap[string, int](nil).IsNil(); !got {
		t.Errorf("nil-backed Map.IsNil() = false, want true")
	}
	if got := NewMap(map[string]int{}).IsNil(); got {
		t.Errorf("empty-but-non-nil Map.IsNil() = true, want false")
	}
	if got := NewMap(map[string]int{"a": 1}).IsNil(); got {
		t.Errorf("populated Map.IsNil() = true, want false")
	}
}

// -----------------------------------------------------------------
// Map.Clone — header independence + shallow per-value copy.
// -----------------------------------------------------------------

func TestMap_Clone_HeaderIndependent(t *testing.T) {
	src := map[string]int{"a": 1, "b": 2}
	view := NewMap(src)

	got := view.Clone()
	if !reflect.DeepEqual(got, src) {
		t.Fatalf("Clone content = %v, want %v", got, src)
	}

	got["a"] = 99
	delete(got, "b")
	if v, _ := view.Get("a"); v != 1 {
		t.Fatalf("view[a] = %d after mutating clone, want 1", v)
	}
	if !view.Has("b") {
		t.Fatalf("view lost key b after mutating clone")
	}

	src["c"] = 3
	if _, ok := got["c"]; ok {
		t.Fatalf("clone gained key c after mutating src; clones must be independent")
	}
}

func TestMap_Clone_NilMap(t *testing.T) {
	view := NewMap[string, int](nil)
	got := view.Clone()
	if got != nil {
		t.Fatalf("Clone of nil-backed Map = %v, want nil", got)
	}
}

// Slice2.Clone and Map2.Clone are exercised in
// examples/gen/go/nested/nested_clone_test.go, where real protobuf
// message types parameterise the views — the same shape generator
// callers see in production.

// -----------------------------------------------------------------
// Slice.Clone / Map.Clone — proto.Message element deep-copy.
//
// Repeated / map fields whose element type comes from a package
// excluded via --exclude_packages, or from a well-known-types
// package such as timestamppb, are routed through Slice / Map
// (not Slice2 / Map2) because no _Const view is generated for them.
// pickCloner must still detect the proto.Message branch and run
// proto.Clone on each element so the cloned slice / map can be
// mutated independently of the view's backing storage.
// -----------------------------------------------------------------

func TestSlice_Clone_ProtoMessageElement_DeepCopy(t *testing.T) {
	src := []*timestamppb.Timestamp{
		{Seconds: 1, Nanos: 100},
		{Seconds: 2, Nanos: 200},
	}
	view := NewSlice(src)

	got := view.Clone()
	if len(got) != 2 {
		t.Fatalf("Clone len = %d, want 2", len(got))
	}
	// Element pointers must differ (deep copy, not shallow).
	for i := range got {
		if got[i] == src[i] {
			t.Fatalf("got[%d] aliases src[%d]; element was not deep-copied", i, i)
		}
		if got[i].Seconds != src[i].Seconds || got[i].Nanos != src[i].Nanos {
			t.Fatalf("got[%d] = %+v, want field-equal to src[%d] = %+v", i, got[i], i, src[i])
		}
	}
	// Mutating the clone's element fields must not reach back into the view.
	got[0].Seconds = 999
	if view.At(0).GetSeconds() != 1 {
		t.Fatalf("view[0].Seconds = %d after mutating clone, want 1", view.At(0).GetSeconds())
	}
}

func TestMap_Clone_ProtoMessageValue_DeepCopy(t *testing.T) {
	src := map[string]*timestamppb.Timestamp{
		"a": {Seconds: 1, Nanos: 100},
		"b": {Seconds: 2, Nanos: 200},
	}
	view := NewMap(src)

	got := view.Clone()
	if len(got) != 2 {
		t.Fatalf("Clone len = %d, want 2", len(got))
	}
	for k, v := range got {
		if v == src[k] {
			t.Fatalf("got[%q] aliases src[%q]; value was not deep-copied", k, k)
		}
		if v.Seconds != src[k].Seconds || v.Nanos != src[k].Nanos {
			t.Fatalf("got[%q] = %+v, want field-equal to src[%q] = %+v", k, v, k, src[k])
		}
	}
	got["a"].Seconds = 999
	gv, _ := view.Get("a")
	if gv.GetSeconds() != 1 {
		t.Fatalf("view[a].Seconds = %d after mutating clone, want 1", gv.GetSeconds())
	}
}

// -----------------------------------------------------------------
// Slice.Clone / Map.Clone — []byte element deep-copy.
//
// repeated bytes / map<…, bytes> fields surface as Slice[[]byte] /
// Map[K, []byte]. The outer slice / map header must be cloned and,
// because []byte itself shares an underlying array, each element's
// backing array must also be detached so byte-level mutation of the
// clone cannot leak into the view.
// -----------------------------------------------------------------

func TestSlice_Clone_BytesElement_DeepCopy(t *testing.T) {
	src := [][]byte{{0x01, 0x02}, {0x03, 0x04}}
	view := NewSlice(src)

	got := view.Clone()
	if !reflect.DeepEqual(got, src) {
		t.Fatalf("Clone content = %v, want %v", got, src)
	}
	// Mutate a byte in the clone; the view must not observe it.
	got[0][0] = 0xFF
	if view.At(0)[0] != 0x01 {
		t.Fatalf("view[0][0] = %#x after mutating clone, want 0x01", view.At(0)[0])
	}
}

func TestMap_Clone_BytesValue_DeepCopy(t *testing.T) {
	src := map[string][]byte{"a": {0x01, 0x02}, "b": {0x03, 0x04}}
	view := NewMap(src)

	got := view.Clone()
	if !reflect.DeepEqual(got, src) {
		t.Fatalf("Clone content = %v, want %v", got, src)
	}
	got["a"][0] = 0xFF
	gv, _ := view.Get("a")
	if gv[0] != 0x01 {
		t.Fatalf("view[a][0] = %#x after mutating clone, want 0x01", gv[0])
	}
}

// -----------------------------------------------------------------
// Benchmarks — Slice.Clone / Map.Clone vs stdlib baselines.
//
// N is sized to exceed the small-slice inlining threshold while
// keeping per-iteration noise low; the metric of interest is wall
// time per Clone() call, not throughput, so b.SetBytes is not used.
// -----------------------------------------------------------------

const benchN = 64

// BenchmarkSlice_Clone exercises every branch of the per-element
// type-switch inside [Slice.Clone] and pins each one against a
// stdlib / hand-rolled baseline of equivalent semantics, so a
// regression that re-introduces per-element funcval dispatch or
// breaks the bulk-copy fast path shows up as a multiple-of-baseline
// slowdown.
//
// Sub-benchmarks are grouped by element shape and laid out as
// {Clone, Baseline} pairs so `go test -bench` output reads as
// adjacent rows for direct comparison:
//
//   - Scalar/Clone     vs Scalar/Baseline_Slices       (default branch, copy → memmove)
//   - ProtoMessage/Clone vs ProtoMessage/Baseline_HandRolled (proto.Message branch)
//   - Bytes/Clone      (no stdlib 1-line equivalent; absolute number is the baseline)
func BenchmarkSlice_Clone(b *testing.B) {
	b.Run("Scalar", func(b *testing.B) {
		src := make([]int, benchN)
		for i := range src {
			src[i] = i
		}
		view := NewSlice(src)

		b.Run("Clone", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = view.Clone()
			}
		})
		b.Run("Baseline_Slices", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = slices.Clone(src)
			}
		})
	})

	b.Run("ProtoMessage", func(b *testing.B) {
		src := make([]*timestamppb.Timestamp, benchN)
		for i := range src {
			src[i] = &timestamppb.Timestamp{Seconds: int64(i), Nanos: int32(i)}
		}
		view := NewSlice(src)

		b.Run("Clone", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = view.Clone()
			}
		})
		b.Run("Baseline_HandRolled", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				out := make([]*timestamppb.Timestamp, len(src))
				for i, v := range src {
					out[i] = proto.Clone(v).(*timestamppb.Timestamp)
				}
				_ = out
			}
		})
	})

	b.Run("Bytes", func(b *testing.B) {
		src := make([][]byte, benchN)
		for i := range src {
			src[i] = []byte{byte(i), byte(i + 1), byte(i + 2), byte(i + 3)}
		}
		view := NewSlice(src)

		b.Run("Clone", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = view.Clone()
			}
		})
	})
}

// BenchmarkMap_Clone is the [Map.Clone] companion to
// [BenchmarkSlice_Clone] — same three element-shape branches, same
// {Clone, Baseline} pairing where a stdlib 1-line equivalent exists.
//
//   - Scalar/Clone     vs Scalar/Baseline_Maps        (default branch, maps.Copy)
//   - ProtoMessage/Clone (no stdlib 1-line equivalent for deep-copy maps)
//   - Bytes/Clone        (ditto)
func BenchmarkMap_Clone(b *testing.B) {
	b.Run("Scalar", func(b *testing.B) {
		src := make(map[int]int, benchN)
		for i := 0; i < benchN; i++ {
			src[i] = i
		}
		view := NewMap(src)

		b.Run("Clone", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = view.Clone()
			}
		})
		b.Run("Baseline_Maps", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = maps.Clone(src)
			}
		})
	})

	b.Run("ProtoMessage", func(b *testing.B) {
		src := make(map[int]*timestamppb.Timestamp, benchN)
		for i := 0; i < benchN; i++ {
			src[i] = &timestamppb.Timestamp{Seconds: int64(i), Nanos: int32(i)}
		}
		view := NewMap(src)

		b.Run("Clone", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = view.Clone()
			}
		})
	})

	b.Run("Bytes", func(b *testing.B) {
		src := make(map[int][]byte, benchN)
		for i := 0; i < benchN; i++ {
			src[i] = []byte{byte(i), byte(i + 1), byte(i + 2), byte(i + 3)}
		}
		view := NewMap(src)

		b.Run("Clone", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = view.Clone()
			}
		})
	})
}
