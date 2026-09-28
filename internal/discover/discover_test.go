package discover

import (
	"strings"
	"testing"
	"time"
)

func static(items ...Item) Source {
	return func() ([]Item, error) { return items, nil }
}

func TestCollect(t *testing.T) {
	got := Collect(nil,
		static(
			Item{Name: "api", Path: "/p/api", Kind: Session, Running: true, External: true},
			Item{Name: "scratch", Path: "/tmp", Kind: Session, Running: true},
		),
		static(
			Item{Name: "web", Path: "/p/web", Kind: Project},
			Item{Name: "api", Path: "/p/api", Kind: Project},
		),
	)

	want := []Item{
		{Name: "api", Path: "/p/api", Kind: Project, Running: true, External: true},
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

func TestCollectSortsByLastUsed(t *testing.T) {
	at := func(min int) time.Time { return time.Unix(0, 0).Add(time.Duration(min) * time.Minute) }
	got := Collect(
		map[string]time.Time{"web": at(5), "api": at(1)},
		static(
			Item{Name: "api", Kind: Session, Running: true, LastUsed: at(3)},
			Item{Name: "idle", Kind: Session, Running: true},
		),
		static(
			Item{Name: "never", Kind: Project},
			Item{Name: "web", Kind: Project},
			Item{Name: "api", Kind: Project},
		),
	)
	var names []string
	for _, it := range got {
		names = append(names, it.Name)
	}
	// web was used last; api's tmux attach (3) beats its record (1); then
	// never-used items, running first.
	if want := "web api idle never"; strings.Join(names, " ") != want {
		t.Errorf("order = %v, want %s", names, want)
	}
}
