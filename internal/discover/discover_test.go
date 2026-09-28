package discover

import "testing"

func static(items ...Item) Source {
	return func() ([]Item, error) { return items, nil }
}

func TestCollect(t *testing.T) {
	got := Collect(
		static(
			Item{Name: "api", Path: "/p/api", Kind: Session, Running: true},
			Item{Name: "scratch", Path: "/tmp", Kind: Session, Running: true},
		),
		static(
			Item{Name: "web", Path: "/p/web", Kind: Project},
			Item{Name: "api", Path: "/p/api", Kind: Project},
		),
	)

	want := []Item{
		{Name: "api", Path: "/p/api", Kind: Project, Running: true},
		{Name: "scratch", Path: "/tmp", Kind: Session, Running: true},
		{Name: "web", Path: "/p/web", Kind: Project},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
