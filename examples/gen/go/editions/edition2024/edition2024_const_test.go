// Hand-written tests for the generated edition-2024 *_Const wrappers.
// Located next to the generated .pb.go / .const.pb.go so `go test`
// automatically exercises every const projection after each
// regeneration.
//
// Edition 2024 defaults features.(pb.go).api_level to API_OPAQUE,
// so the generated Go structs hide their fields behind Set*/Has*/
// Clear*/Get* accessors. This file is the single most important
// validation that protoc-gen-go-const's Get*-only forward model is
// API-level-agnostic: if the const layer compiles and agrees with
// the concrete getters here, it will work under OPEN too.
package edition2024

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

// newTag is the opaque-API counterpart of a struct literal.
func newTag(k, v string) *Tag {
	t := &Tag{}
	t.SetKey(k)
	t.SetValue(v)
	return t
}

func TestEdition2024_AsConst_Identity(t *testing.T) {
	m := &Person{}
	m.SetName("alice")
	m.SetAge(42)
	m.SetId("primary-id")
	m.SetFavColor(Color_COLOR_GREEN)
	m.SetHomeTag(newTag("home", "v"))
	m.SetTags([]*Tag{newTag("k1", "v1"), newTag("k2", "v2")})
	m.SetLabels(map[string]*Tag{"primary": newTag("pk", "pv")})

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

func TestEdition2024_Const_NonComparable(t *testing.T) {
	if reflect.TypeOf(Person_Const{}).Comparable() {
		t.Error("Person_Const must be non-comparable")
	}
	if reflect.TypeOf(Tag_Const{}).Comparable() {
		t.Error("Tag_Const must be non-comparable")
	}
}

func TestEdition2024_AsConst_NilReceiver(t *testing.T) {
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
