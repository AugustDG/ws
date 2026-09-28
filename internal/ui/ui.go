// Package ui holds the colors and labels shared by the picker and ws ls.
package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/AugustDG/ws/internal/discover"
)

var (
	Dim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	running  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	external = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// State is an item's state label: running, external (running but not
// started by ws) or stopped.
func State(it discover.Item) string {
	switch {
	case it.External:
		return "external"
	case it.Running:
		return "running"
	}
	return "stopped"
}

// StateStyle is the color for an item's state.
func StateStyle(it discover.Item) lipgloss.Style {
	switch {
	case it.External:
		return external
	case it.Running:
		return running
	}
	return Dim
}
