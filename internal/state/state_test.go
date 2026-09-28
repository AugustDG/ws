package state

import (
	"slices"
	"testing"
)

func TestLastIsNotASession(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, ok, err := s.LoadLast(); ok || err != nil {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	if err := s.Save("proj", Session{Root: "/r"}); err != nil {
		t.Fatal(err)
	}
	want := Last{Name: "proj", Root: "/r"}
	if err := s.SaveLast(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LoadLast()
	if err != nil || !ok || got != want {
		t.Fatalf("LoadLast = %+v, %v, %v", got, ok, err)
	}
	names, err := s.Names()
	if err != nil || !slices.Equal(names, []string{"proj"}) {
		t.Errorf("Names = %v, %v; want only proj", names, err)
	}
}
