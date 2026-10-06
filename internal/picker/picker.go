// Package picker is the fuzzy finder `ws` opens with no arguments.
package picker

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/AugustDG/ws/internal/discover"
)

// Action is what the user chose to do with the selected item.
type Action int

const (
	None Action = iota
	Open
	Stop
)

// Choice is the picker's result.
type Choice struct {
	Action Action
	Item   discover.Item
}

// Options describe the machine the picker runs on.
type Options struct {
	// Here names this machine's group: "local", or on a host reached
	// with ws ssh, the name it was reached by.
	Here string
	// Remote is set on a host reached with ws ssh, which colors its group
	// like the other hosts.
	Remote bool
}

// Run shows items, grouped by machine, and returns the user's choice.
// display shortens paths.
func Run(items []discover.Item, display func(string) string, opts Options) (Choice, error) {
	m := newModel(items, display, opts)
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return Choice{}, err
	}
	return final.(model).choice, nil
}

var (
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleRunning  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleExternal = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleMatch    = lipgloss.NewStyle().Underline(true)
	// Machine colors match the tmux status bar: green here, amber on hosts.
	styleLocalHost  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FBB0"))
	styleRemoteHost = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFB000"))
)

// row is one line: a machine's header, or an item under it.
type row struct {
	item    int // index into items; -1 for a header that's only a label
	machine string
	header  bool
	matched []int // matched rune indexes in the name
}

func (r row) selectable() bool { return r.item >= 0 }

type model struct {
	input    textinput.Model
	items    []discover.Item
	haystack []string // what fuzzy matching runs against, per item
	rows     []row
	cursor   int
	height   int
	width    int
	display  func(string) string
	opts     Options
	choice   Choice
	now      time.Time
	query    string // what rows were built for
}

func newModel(items []discover.Item, display func(string) string, opts Options) model {
	in := textinput.New()
	in.Prompt = "❯ "
	in.Placeholder = "project, session or host"
	in.Focus()

	m := model{input: in, items: items, display: display, opts: opts, height: 20, width: 100, now: time.Now()}
	for _, it := range items {
		m.haystack = append(m.haystack, it.Name+" "+display(it.Path)+" "+it.Machine)
	}
	m.filter()
	return m
}

// filter rebuilds the rows when the query changed: this machine's group
// first, then the others in the order their most recent item appears. A
// machine's Host item is its header; without one the header is a label.
// Other updates, like the input's cursor blinking, keep the selection.
func (m *model) filter() {
	q := strings.TrimSpace(m.input.Value())
	if m.rows != nil && q == m.query {
		return
	}
	m.query = q
	matched := map[int][]int{}
	var order []int // item indexes, best match first
	if q == "" {
		for i := range m.items {
			order = append(order, i)
			matched[i] = nil
		}
	} else {
		for _, mt := range fuzzy.Find(q, m.haystack) {
			order = append(order, mt.Index)
			matched[mt.Index] = mt.MatchedIndexes
		}
	}

	machines := []string{""}
	header := map[string]int{}
	for i, it := range m.items {
		if !slices.Contains(machines, it.Machine) {
			machines = append(machines, it.Machine)
		}
		if it.Kind == discover.Host {
			header[it.Machine] = i
		}
	}

	m.rows = []row{}
	for _, machine := range machines {
		var under []row
		for _, i := range order {
			if it := m.items[i]; it.Machine == machine && it.Kind != discover.Host {
				under = append(under, row{item: i, machine: machine, matched: matched[i]})
			}
		}
		h, hasHeader := header[machine]
		_, headerMatched := matched[h]
		if len(under) == 0 && !(hasHeader && headerMatched) {
			continue
		}
		head := row{item: -1, machine: machine, header: true}
		if hasHeader && machine != "" {
			head.item, head.matched = h, matched[h]
		}
		m.rows = append(append(m.rows, head), under...)
	}

	// Start on the best match, or the first item when not filtering.
	m.cursor = m.first()
}

// first is the row the cursor starts on: the top item, else a header.
func (m model) first() int {
	for i, r := range m.rows {
		if !r.header {
			return i
		}
	}
	for i, r := range m.rows {
		if r.selectable() {
			return i
		}
	}
	return 0
}

// step moves the cursor by dir over selectable rows.
func (m *model) step(dir int) {
	for i := m.cursor + dir; i >= 0 && i < len(m.rows); i += dir {
		if m.rows[i].selectable() {
			m.cursor = i
			return
		}
	}
}

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = max(msg.Height-3, 1)
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			return m.choose(Open)
		case "ctrl+x":
			return m.choose(Stop)
		case "up", "ctrl+p", "ctrl+k":
			m.step(-1)
			return m, nil
		case "down", "ctrl+n", "ctrl+j":
			m.step(1)
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.filter()
	return m, cmd
}

func (m model) choose(a Action) (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.rows) || !m.rows[m.cursor].selectable() {
		return m, nil
	}
	it := m.items[m.rows[m.cursor].item]
	// Only this machine's sessions can be stopped from here.
	if a == Stop && (!it.Running || !it.Here()) {
		return m, nil
	}
	m.choice = Choice{Action: a, Item: it}
	return m, tea.Quit
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.input.View() + "\n")

	start := 0
	if m.cursor >= m.height {
		start = m.cursor - m.height + 1
	}
	end := min(start+m.height, len(m.rows))
	for i := start; i < end; i++ {
		b.WriteString(m.row(m.rows[i], i == m.cursor) + "\n")
	}
	items := 0
	for _, it := range m.items {
		if it.Kind != discover.Host {
			items++
		}
	}
	shown := 0
	for _, r := range m.rows {
		if !r.header {
			shown++
		}
	}
	b.WriteString(styleDim.Render(fmt.Sprintf("%d/%d  enter open · ctrl-x stop · esc quit", shown, items)))
	return b.String()
}

func (m model) row(r row, selected bool) string {
	if r.header {
		return m.headerRow(r, selected)
	}
	it := m.items[r.item]
	marker := "  "
	if it.Running {
		marker = markerStyle(it).Render("● ")
	}
	name := highlight(it.Name, r.matched)
	if selected {
		name = styleSelected.Render("› ") + styleSelected.Render(name)
	} else {
		name = "  " + name
	}
	head := "  " + marker + name + "  " + it.Kind.String() + "  "
	path := truncateLeft(m.display(it.Path), m.width-lipgloss.Width(head))
	return "  " + marker + name + "  " + styleDim.Render(it.Kind.String()+"  "+path)
}

// headerRow names a machine in its color. A host's header says when it was
// last connected to, which is also how fresh its items are.
func (m model) headerRow(r row, selected bool) string {
	name, style, note := m.opts.Here, styleLocalHost, ""
	if name == "" {
		name = "local"
	}
	if r.machine == "" {
		if m.opts.Remote {
			style, note = styleRemoteHost, "this host"
		}
	} else {
		name = r.machine
		if r.machine != remoteLocal {
			style = styleRemoteHost
		}
		if r.item >= 0 {
			it := m.items[r.item]
			name = highlight(it.Name, r.matched)
			if !it.LastUsed.IsZero() {
				note = "seen " + ago(m.now.Sub(it.LastUsed))
			}
			if r.machine == remoteLocal {
				note = "back to where you left"
			}
		}
	}
	prefix := "▌ "
	if selected {
		prefix = styleSelected.Render("› ")
	}
	line := prefix + style.Render(name)
	if note != "" {
		line += styleDim.Render("  · " + note)
	}
	return line
}

// remoteLocal is the group a host shows the connecting machine's items in.
const remoteLocal = "local"

// ago is a duration in the largest unit that fits: "4m ago".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// truncateLeft keeps the end of s, which is the informative part of a path.
func truncateLeft(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return ""
	}
	return "…" + string(r[len(r)-width+1:])
}

// highlight underlines matched characters that fall within the name.
func highlight(name string, matched []int) string {
	if len(matched) == 0 {
		return name
	}
	hit := map[int]bool{}
	for _, i := range matched {
		hit[i] = true
	}
	var b strings.Builder
	for i, r := range []rune(name) {
		if hit[i] {
			b.WriteString(styleMatch.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// markerStyle colors a running item's dot: green when ws started it,
// yellow when it was started some other way, dim when it's a host's last
// report.
func markerStyle(it discover.Item) lipgloss.Style {
	if it.Cached { // as last reported; may have changed since
		return styleDim
	}
	if it.External {
		return styleExternal
	}
	return styleRunning
}
