package state

import (
	"slices"
	"testing"
)

func TestUsedIsNotASession(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, _, ok, err := s.Last(); ok || err != nil {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	if err := s.Save("proj", Session{Root: "/r"}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"old", "proj"} {
		if err := s.RecordUse(n, "/"+n); err != nil {
			t.Fatal(err)
		}
	}
	name, use, ok, err := s.Last()
	if err != nil || !ok || name != "proj" || use.Root != "/proj" {
		t.Fatalf("Last = %q %+v %v %v, want proj", name, use, ok, err)
	}
	if used, _ := s.Used(); len(used) != 2 {
		t.Errorf("Used = %v, want old and proj", used)
	}
	names, err := s.Names()
	if err != nil || !slices.Equal(names, []string{"proj"}) {
		t.Errorf("Names = %v, %v; want only proj", names, err)
	}
}
