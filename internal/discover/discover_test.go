package discover

import (
	"strings"
	"testing"
	"time"

	"github.com/AugustDG/ws/internal/state"
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

func TestCollectKeepsRemotesApart(t *testing.T) {
	at := func(min int) time.Time { return time.Unix(0, 0).Add(time.Duration(min) * time.Minute) }
	got := Collect(
		map[string]time.Time{"box": at(9)},
		static(Item{Name: "box", Kind: Session, Running: true, LastUsed: at(1)}),
		static(Item{Name: "box", Kind: Remote, Machine: "box", Host: "box", LastUsed: at(5)}),
	)
	if len(got) != 2 {
		t.Fatalf("a remote merged with a local session: %+v", got)
	}
	// used applies to the local box only, so it sorts first.
	if got[0].Kind != Session || got[1].Kind != Remote || !got[1].LastUsed.Equal(at(5)) {
		t.Errorf("got %+v", got)
	}
	if got[1].State() != "remote" {
		t.Errorf("remote state %q", got[1].State())
	}
}

func TestRemotes(t *testing.T) {
	st := state.Store{Dir: t.TempDir()}
	for _, r := range [][2]string{{"box", "dev"}, {"box", "old"}, {"pi", ""}} {
		if err := st.RecordRemote(r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	report := []Item{{Name: "dev", Kind: Session, Running: true}, {Name: "app", Kind: Project, Path: "/srv/app"}}
	if err := st.SaveHostItems("box", report); err != nil {
		t.Fatal(err)
	}
	items, err := Remotes(st)()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Machine+"/"+it.Name+"/"+it.Kind.String())
		if it.Here() || it.Host != it.Machine {
			t.Errorf("%+v isn't placed on its host", it)
		}
	}
	// pi was connected to last, then box. box reported, so its report
	// replaces the targets it was given; pi never did.
	if err := st.RecordRemote("pi", "work"); err != nil {
		t.Fatal(err)
	}
	items, err = Remotes(st)()
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, it := range items {
		got = append(got, it.Machine+"/"+it.Name+"/"+it.Kind.String())
	}
	want := "pi/pi/host pi/work/remote box/box/host box/dev/session box/app/project"
	if strings.Join(got, " ") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, " "), want)
	}
	for _, it := range items {
		if it.Name == "app" && (!it.Cached || it.Target != "app" || it.Label() != "box project") {
			t.Errorf("reported item %+v", it)
		}
	}
}
