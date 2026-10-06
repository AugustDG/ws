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

func TestRemotes(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if r, err := s.Remotes(); r != nil || err != nil {
		t.Fatalf("empty store: %v, %v", r, err)
	}
	for _, r := range [][2]string{{"box", ""}, {"box", "dev"}, {"pi", ""}, {"box", ""}} {
		if err := s.RecordRemote(r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	remotes, err := s.Remotes()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range remotes {
		got = append(got, r.Host+":"+r.Target)
	}
	// Reconnecting moves an entry to the front instead of adding another.
	if want := []string{"box:", "pi:", "box:dev"}; !slices.Equal(got, want) {
		t.Errorf("remotes = %v, want %v", got, want)
	}
	if used, _ := s.Used(); len(used) != 0 {
		t.Errorf("remotes leaked into Used: %v", used)
	}
	if names, _ := s.Names(); len(names) != 0 {
		t.Errorf("remotes listed as sessions: %v", names)
	}
}
