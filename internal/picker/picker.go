// Package picker is the fuzzy finder `ws` opens with no arguments.
package picker

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/AugustDG/ws/internal/discover"
	"github.com/AugustDG/ws/internal/ui"
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

// Run shows items and returns the user's choice. display shortens paths.
func Run(items []discover.Item, display func(string) string) (Choice, error) {
	m := newModel(items, display)
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return Choice{}, err
	}
	return final.(model).choice, nil
}

var (
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleMatch    = lipgloss.NewStyle().Underline(true)
)

type model struct {
	input    textinput.Model
	items    []discover.Item
	haystack []string // what fuzzy matching runs against, per item
	matches  []fuzzy.Match
	cursor   int
	height   int
	width    int
	display  func(string) string
	choice   Choice
}

func newModel(items []discover.Item, display func(string) string) model {
	in := textinput.New()
	in.Prompt = "❯ "
	in.Placeholder = "project or session"
	in.Focus()

	m := model{input: in, items: items, display: display, height: 20, width: 100}
	for _, it := range items {
		m.haystack = append(m.haystack, it.Name+" "+display(it.Path))
	}
	m.filter()
	return m
}

func (m *model) filter() {
	q := strings.TrimSpace(m.input.Value())
	if q == "" {
		m.matches = make([]fuzzy.Match, len(m.items))
		for i := range m.items {
			m.matches[i] = fuzzy.Match{Index: i}
		}
	} else {
		m.matches = fuzzy.Find(q, m.haystack)
	}
	m.cursor = min(m.cursor, max(len(m.matches)-1, 0))
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
			m.cursor = max(m.cursor-1, 0)
			return m, nil
		case "down", "ctrl+n", "ctrl+j":
			m.cursor = min(m.cursor+1, max(len(m.matches)-1, 0))
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.filter()
	return m, cmd
}

func (m model) choose(a Action) (tea.Model, tea.Cmd) {
	if len(m.matches) == 0 {
		return m, nil
	}
	it := m.items[m.matches[m.cursor].Index]
	if a == Stop && !it.Running {
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
	end := min(start+m.height, len(m.matches))
	for i := start; i < end; i++ {
		b.WriteString(m.row(m.matches[i], i == m.cursor) + "\n")
	}
	b.WriteString(ui.Dim.Render(fmt.Sprintf("%d/%d  enter open · ctrl-x stop · esc quit", len(m.matches), len(m.items))))
	return b.String()
}

func (m model) row(match fuzzy.Match, selected bool) string {
	it := m.items[match.Index]
	marker := "  "
	if it.Running {
		marker = ui.StateStyle(it).Render("● ")
	}
	name := highlight(it.Name, match.MatchedIndexes)
	if selected {
		name = styleSelected.Render("› ") + styleSelected.Render(name)
	} else {
		name = "  " + name
	}
	head := marker + name + "  " + it.Kind.String() + "  "
	path := truncateLeft(m.display(it.Path), m.width-lipgloss.Width(head))
	return marker + name + "  " + ui.Dim.Render(it.Kind.String()+"  "+path)
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
