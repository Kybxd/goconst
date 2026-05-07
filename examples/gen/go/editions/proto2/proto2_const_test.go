// Hand-written tests for the generated proto2 *_Const wrappers.
// Located next to the generated .pb.go / .const.pb.go so `go test`
// automatically exercises every const projection after each
// regeneration.
package proto2

import (
	"reflect"
	"testing"

	goconst "github.com/Kybxd/goconst"
)

// Compile-time checks: Person / Person_Tag / Person_Note all satisfy
// goconst.Constable against their matching *_Const types, and the
// *_Const values are themselves non-comparable thanks to the
// embedded goconst.DoNotCompare sentinel.
var (
	_ goconst.Constable[Person_Const]      = (*Person)(nil)
	_ goconst.Constable[Person_Tag_Const]  = (*Person_Tag)(nil)
	_ goconst.Constable[Person_Note_Const] = (*Person_Note)(nil)

	_ Person_Const      = (*Person)(nil).AsConst()
	_ Person_Tag_Const  = (*Person_Tag)(nil).AsConst()
	_ Person_Note_Const = (*Person_Note)(nil).AsConst()
)

// TestProto2_AsConst_Identity verifies that every forwarded Get*
// method on Person_Const returns exactly what the concrete getter
// returns, including on proto2-specific constructs (required /
// optional-with-default / group).
func TestProto2_AsConst_Identity(t *testing.T) {
	name := "alice"
	age := int32(30)
	zip := "10001"
	fav := Color_COLOR_BLUE
	body := "hello"
	prio := int32(9)

	m := &Person{
		Name:     &name,
		Age:      &age,
		Zip:      &zip,
		FavColor: &fav,
		Tags: []*Person_Tag{
			{Key: strptr("k1"), Value: strptr("v1")},
			{Key: strptr("k2"), Value: strptr("v2")},
		},
		Labels: map[string]*Person_Tag{
			"home": {Key: strptr("kh"), Value: strptr("vh")},
		},
		Note: &Person_Note{Body: &body, Priority: &prio},
	}
	c := m.AsConst()

	if got, want := c.GetName(), m.GetName(); got != want {
		t.Errorf("Name: got %q want %q", got, want)
	}
	if got, want := c.GetAge(), m.GetAge(); got != want {
		t.Errorf("Age: got %d want %d", got, want)
	}
	if got, want := c.GetZip(), m.GetZip(); got != want {
		t.Errorf("Zip: got %q want %q", got, want)
	}
	if got, want := c.GetFavColor(), m.GetFavColor(); got != want {
		t.Errorf("FavColor: got %v want %v", got, want)
	}

	// Repeated message forwarded through Slice2: length + element
	// identity should match the concrete slice.
	tags := c.GetTags()
	if tags.Len() != len(m.GetTags()) {
		t.Fatalf("Tags.Len: got %d want %d", tags.Len(), len(m.GetTags()))
	}
	for i := 0; i < tags.Len(); i++ {
		if got, want := tags.At(i).GetKey(), m.GetTags()[i].GetKey(); got != want {
			t.Errorf("Tags[%d].Key: got %q want %q", i, got, want)
		}
	}

	// Map with message value forwarded through Map2.
	labels := c.GetLabels()
	if labels.Len() != len(m.GetLabels()) {
		t.Fatalf("Labels.Len: got %d want %d", labels.Len(), len(m.GetLabels()))
	}
	if got, ok := labels.Get("home"); !ok {
		t.Error("Labels[home]: missing")
	} else if got.GetValue() != "vh" {
		t.Errorf("Labels[home].Value: got %q want %q", got.GetValue(), "vh")
	}

	// Proto2 group: nested message reached through GetNote(), which
	// must return Person_Note_Const (not *Person_Note).
	note := c.GetNote()
	if note.GetBody() != m.GetNote().GetBody() {
		t.Errorf("Note.Body: got %q want %q", note.GetBody(), m.GetNote().GetBody())
	}
	if note.GetPriority() != m.GetNote().GetPriority() {
		t.Errorf("Note.Priority: got %d want %d", note.GetPriority(), m.GetNote().GetPriority())
	}
}

// TestProto2_AsConst_Defaults pins the expectation that proto2
// [default = …] values surface through the const layer untouched,
// because the const Get* methods simply forward to the concrete
// getters which already apply the defaults.
func TestProto2_AsConst_Defaults(t *testing.T) {
	c := (&Person{Name: strptr("bob")}).AsConst()
	if got := c.GetAge(); got != Default_Person_Age {
		t.Errorf("default Age: got %d want %d", got, Default_Person_Age)
	}
	if got := c.GetZip(); got != Default_Person_Zip {
		t.Errorf("default Zip: got %q want %q", got, Default_Person_Zip)
	}
	if got := c.GetFavColor(); got != Default_Person_FavColor {
		t.Errorf("default FavColor: got %v want %v", got, Default_Person_FavColor)
	}
	// group: unset Note, Get* still nil-safe
	if got := c.GetNote().GetBody(); got != "" {
		t.Errorf("unset Note.Body: got %q want \"\"", got)
	}
}

// TestProto2_Const_NonComparable enforces the goconst.DoNotCompare
// contract at runtime: the wrapper struct must report its reflect
// Type as non-comparable, preventing accidental `==` / map-key use.
func TestProto2_Const_NonComparable(t *testing.T) {
	if reflect.TypeOf(Person_Const{}).Comparable() {
		t.Error("Person_Const must be non-comparable")
	}
	if reflect.TypeOf(Person_Tag_Const{}).Comparable() {
		t.Error("Person_Tag_Const must be non-comparable")
	}
	if reflect.TypeOf(Person_Note_Const{}).Comparable() {
		t.Error("Person_Note_Const must be non-comparable")
	}
}

// TestProto2_AsConst_NilReceiver ensures AsConst + Get* are nil-safe
// all the way down, including through the group field.
func TestProto2_AsConst_NilReceiver(t *testing.T) {
	var m *Person
	c := m.AsConst()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Get* on nil-backed Person_Const panicked: %v", r)
		}
	}()

	_ = c.GetName()
	_ = c.GetAge()
	_ = c.GetZip()
	_ = c.GetFavColor()
	_ = c.GetTags().Len()
	_ = c.GetLabels().Len()
	_ = c.GetNote().GetBody()
}

func strptr(s string) *string { return &s }
