// Package project loads ws configuration: global settings, project files and
// layout templates.
//
//	~/.config/ws/
//	  config.yaml          settings (optional)
//	  projects/<name>.yaml one file per project
//	  layouts/<name>.yaml  reusable layout templates
//	<repo>/.ws.yaml        project file kept in the repo; wins over projects/
package project

import (
	"github.com/AugustDG/ws/internal/layout"
)

// LocalFile is the name of a project file kept inside the project's root.
const LocalFile = ".ws.yaml"

// DefaultLayout is used when neither the project nor the settings name one.
const DefaultLayout = "default"

// Project is one workspace: where it lives and how its session looks.
type Project struct {
	Name      string            `yaml:"name,omitempty"`
	Root      string            `yaml:"root,omitempty"`
	Layout    string            `yaml:"layout,omitempty"`
	Windows   []layout.Window   `yaml:"windows,omitempty"`
	Worktrees *Worktrees        `yaml:"worktrees,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	OnStart   string            `yaml:"on_start,omitempty"`
	OnStop    string            `yaml:"on_stop,omitempty"`

	// File is where the project was loaded from; empty for a directory
	// with no project file.
	File string `yaml:"-"`
}

// Worktrees says where for_each: worktree gets its extra checkouts.
type Worktrees struct {
	Source string `yaml:"source"`
	Count  int    `yaml:"count,omitempty"`
}

// Settings is config.yaml.
type Settings struct {
	DefaultLayout string `yaml:"default_layout,omitempty"`
}

// builtinDefault is the layout for plain directories: one window, one pane.
var builtinDefault = layout.File{Windows: []layout.Window{{Name: "main"}}}
