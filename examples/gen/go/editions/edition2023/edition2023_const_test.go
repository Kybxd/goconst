// Hand-written tests for the generated edition-2023 *_Const wrappers.
// Located next to the generated .pb.go / .const.pb.go so `go test`
// automatically exercises every const projection after each
// regeneration.
//
// Edition 2023 keeps the OPEN API style (exported struct fields,
// *T for EXPLICIT presence, T for IMPLICIT, *T for LEGACY_REQUIRED),
// so we can still construct messages with struct literals.
package edition2023

import (
	"reflect"
	"testing"

	goconst "github.com/Kybxd/goconst"
)

var (
	_ goconst.Constable[Person_Const] = (*Person)(nil)
	_ goconst.Constable[Tag_Const]    = (*Tag)(nil)

	_ Person_Const = (*Person)(nil).AsConst()
	_ Tag_Const    = (*Tag)(nil).AsConst()
)

func TestEdition2023_AsConst_Identity(t *testing.T) {
	name := "alice"
	id := "primary-id"
	fav := Color_COLOR_GREEN

	// Under EXPLICIT file-level presence every scalar field on Tag is
	// also *string, so we build Tags through a small helper.
	tag := func(k, v string) *Tag { return &Tag{Key: &k, Value: &v} }

	m := &Person{
		Name:     &name, // EXPLICIT  -> *string
		Age:      42,    // IMPLICIT  -> int32
		Id:       &id,   // LEGACY_REQUIRED -> *string
		FavColor: &fav,  // EXPLICIT, CLOSED enum -> *Color
		HomeTag:  tag("home", "v"),
		Tags:     []*Tag{tag("k1", "v1"), tag("k2", "v2")},
		Labels:   map[string]*Tag{"primary": tag("pk", "pv")},
	}
	c := m.AsConst()

	if got, want := c.GetName(), m.GetName(); got != want {
		t.Errorf("Name: got %q want %q", got, want)
	}
	if got, want := c.GetAge(), m.GetAge(); got != want {
		t.Errorf("Age: got %d want %d", got, want)
	}
	if got, want := c.GetId(), m.GetId(); got != want {
		t.Errorf("Id: got %q want %q", got, want)
	}
	if got, want := c.GetFavColor(), m.GetFavColor(); got != want {
		t.Errorf("FavColor: got %v want %v", got, want)
	}
	if got, want := c.GetHomeTag().GetKey(), m.GetHomeTag().GetKey(); got != want {
		t.Errorf("HomeTag.Key: got %q want %q", got, want)
	}

	tags := c.GetTags()
	if tags.Len() != len(m.GetTags()) {
		t.Fatalf("Tags.Len: got %d want %d", tags.Len(), len(m.GetTags()))
	}
	for i := 0; i < tags.Len(); i++ {
		if got, want := tags.At(i).GetKey(), m.GetTags()[i].GetKey(); got != want {
			t.Errorf("Tags[%d].Key: got %q want %q", i, got, want)
		}
	}

	labels := c.GetLabels()
	if labels.Len() != len(m.GetLabels()) {
		t.Fatalf("Labels.Len: got %d want %d", labels.Len(), len(m.GetLabels()))
	}
	if got, ok := labels.Get("primary"); !ok {
		t.Error("Labels[primary]: missing")
	} else if got.GetValue() != "pv" {
		t.Errorf("Labels[primary].Value: got %q want %q", got.GetValue(), "pv")
	}
}

func TestEdition2023_Const_NonComparable(t *testing.T) {
	if reflect.TypeOf(Person_Const{}).Comparable() {
		t.Error("Person_Const must be non-comparable")
	}
	if reflect.TypeOf(Tag_Const{}).Comparable() {
		t.Error("Tag_Const must be non-comparable")
	}
}

func TestEdition2023_AsConst_NilReceiver(t *testing.T) {
	var m *Person
	c := m.AsConst()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Get* on nil-backed Person_Const panicked: %v", r)
		}
	}()

	_ = c.GetName()
	_ = c.GetAge()
	_ = c.GetId()
	_ = c.GetFavColor()
	_ = c.GetHomeTag().GetKey()
	_ = c.GetTags().Len()
	_ = c.GetLabels().Len()
}
