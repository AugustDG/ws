package picker

import (
	"strings"
	"testing"

	"github.com/AugustDG/ws/internal/discover"
)

// layout renders rows as "header" / "  item", with "*" on rows the cursor
// can't stop on.
func layout(m model) string {
	var lines []string
	for _, r := range m.rows {
		name := "?"
		if r.item >= 0 {
			name = m.items[r.item].Name
		}
		if r.header {
			name = "[" + r.machine + "]"
			if r.item >= 0 {
				name += " " + m.items[r.item].Name
			}
		} else {
			name = "  " + name
		}
		if !r.selectable() {
			name += " *"
		}
		lines = append(lines, name)
	}
	return strings.Join(lines, "\n")
}

func TestGroups(t *testing.T) {
	items := []discover.Item{
		{Name: "pi", Kind: discover.Host, Machine: "pi", Host: "pi"},
		{Name: "viber", Kind: discover.Project},
		{Name: "box", Kind: discover.Host, Machine: "box", Host: "box"},
		{Name: "api", Kind: discover.Session, Machine: "box", Host: "box", Cached: true},
		{Name: "misc", Kind: discover.Project},
	}
	m := newModel(items, func(s string) string { return s }, Options{Here: "local"})
	// This machine first; then hosts in the order they were last used.
	want := `[] *
  viber
  misc
[pi] pi
[box] box
  api`
	if got := layout(m); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if m.cursor != 1 {
		t.Errorf("cursor on row %d, want the first item", m.cursor)
	}
	m.step(-1)
	if m.cursor != 1 {
		t.Errorf("cursor moved onto the label header: %d", m.cursor)
	}
	m.step(1)
	m.step(1)
	if m.cursor != 3 {
		t.Errorf("cursor skipped the pi header: %d", m.cursor)
	}

	// Updates that don't change the query, like the input's cursor
	// blinking, keep the selection.
	next, _ := m.Update(m.input.Cursor.BlinkCmd()())
	if got := next.(model).cursor; got != 3 {
		t.Errorf("a blink moved the cursor to %d", got)
	}

	// Searching drops the groups: rows rank by match, hosts included.
	m.input.SetValue("box")
	m.filter()
	if got := layout(m); got != "  box\n  api" {
		t.Errorf("filtered:\n%s", got)
	}
	m.input.SetValue("zzz")
	m.filter()
	if len(m.rows) != 0 {
		t.Errorf("no match still shows %q", layout(m))
	}
	if _, cmd := m.choose(Open); cmd != nil {
		t.Error("chose with nothing shown")
	}
}

func TestStopOnlyHere(t *testing.T) {
	items := []discover.Item{
		{Name: "api", Kind: discover.Session, Running: true, Machine: "box", Host: "box"},
	}
	m := newModel(items, func(s string) string { return s }, Options{})
	if _, cmd := m.choose(Stop); cmd != nil {
		t.Error("stopped another machine's session")
	}
}

func TestSearchRanksAcrossMachines(t *testing.T) {
	items := []discover.Item{
		{Name: "graphite", Kind: discover.Project},
		{Name: "box", Kind: discover.Host, Machine: "box", Host: "box"},
		{Name: "api", Kind: discover.Session, Machine: "box", Host: "box"},
	}
	m := newModel(items, func(s string) string { return s }, Options{Here: "local"})
	m.input.SetValue("api")
	m.filter()
	// box's api matches better than the local project, so it comes first
	// although this machine's group would.
	if got := layout(m); got != "  api\n  graphite" {
		t.Errorf("ranked:\n%s", got)
	}
	if m.cursor != 0 {
		t.Errorf("cursor on %d, want the best match", m.cursor)
	}
	m.input.SetValue("")
	m.filter()
	if !m.rows[0].header {
		t.Error("clearing the query didn't bring the groups back")
	}
}
