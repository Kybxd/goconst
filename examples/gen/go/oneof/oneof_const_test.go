// Unit tests for the generated Event_Const view. Specifically verifies
// that oneof-arm getters return the right arm's value and zero-values for
// the others, and that the cross-file message arm returns an Address_Const
// (not *nested.Address).
package oneof

import (
	"testing"

	goconst "github.com/Kybxd/goconst"
	nested "github.com/Kybxd/goconst/examples/gen/go/nested"
)

var (
	_ Event_Const                    = (*Event)(nil).AsConst()
	_ goconst.Constable[Event_Const] = (*Event)(nil)
)

// TestEvent_OneofArm_Note: when payload=Note, only GetNote() is populated.
func TestEvent_OneofArm_Note(t *testing.T) {
	e := &Event{Id: "e1", Payload: &Event_Note{Note: "hello"}}
	c := e.AsConst()

	if c.GetId() != "e1" {
		t.Errorf("Id: got %q", c.GetId())
	}
	if c.GetNote() != "hello" {
		t.Errorf("Note: got %q, want hello", c.GetNote())
	}
	if c.GetCount() != 0 {
		t.Errorf("Count arm should be zero when not selected: got %d", c.GetCount())
	}
	// GetLocation must return a typed non-nil interface whose backing value
	// is a nil *Address; its scalar getters must all return zero values.
	loc := c.GetLocation()
	if loc == nil {
		t.Fatal("GetLocation must return typed non-nil interface")
	}
	if loc.GetCity() != "" {
		t.Errorf("Location on wrong arm: GetCity=%q, want \"\"", loc.GetCity())
	}
}

// TestEvent_OneofArm_Count: when payload=Count, only GetCount() is populated.
func TestEvent_OneofArm_Count(t *testing.T) {
	e := &Event{Id: "e2", Payload: &Event_Count{Count: 42}}
	c := e.AsConst()
	if c.GetCount() != 42 {
		t.Errorf("Count: got %d want 42", c.GetCount())
	}
	if c.GetNote() != "" {
		t.Errorf("Note arm: got %q want \"\"", c.GetNote())
	}
}

// TestEvent_OneofArm_Location: when payload=Location, the _Const view must
// project it through AsConst and return a nested.Address_Const.
func TestEvent_OneofArm_Location(t *testing.T) {
	addr := &nested.Address{Street: "Elm", City: "Paris", Zip: "75000"}
	e := &Event{Id: "e3", Payload: &Event_Location{Location: addr}}
	c := e.AsConst()

	loc := c.GetLocation()
	if loc == nil {
		t.Fatal("Location: nil _Const")
	}
	// Static-type assertion: cross-file arm must return the _Const view.
	var _ nested.Address_Const = loc
	if loc.GetCity() != "Paris" || loc.GetStreet() != "Elm" || loc.GetZip() != "75000" {
		t.Errorf("Location mismatch: %+v", loc)
	}
}

// TestEvent_OneofArm_UnsetAll: empty Event yields zeros on every arm.
func TestEvent_OneofArm_UnsetAll(t *testing.T) {
	c := (&Event{}).AsConst()
	if c.GetNote() != "" || c.GetCount() != 0 {
		t.Errorf("unset arms not zero: note=%q count=%d", c.GetNote(), c.GetCount())
	}
	loc := c.GetLocation()
	if loc == nil {
		t.Fatal("GetLocation on empty Event must return typed non-nil")
	}
	if loc.GetCity() != "" {
		t.Errorf("unset Location.City: got %q", loc.GetCity())
	}
}
