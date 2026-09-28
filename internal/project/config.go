package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/AugustDG/ws/internal/layout"
)

// Config is a loaded config directory.
type Config struct {
	Dir      string
	Home     string
	Settings Settings
}

// Load reads settings from $WS_CONFIG_DIR, $XDG_CONFIG_HOME/ws or
// ~/.config/ws, in that order. A missing directory is an empty config.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := os.Getenv("WS_CONFIG_DIR")
	if dir == "" {
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		dir = filepath.Join(base, "ws")
	}

	c := &Config{Dir: dir, Home: home}
	if err := readYAML(filepath.Join(dir, "config.yaml"), &c.Settings); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return c, nil
}

func (c *Config) ProjectsDir() string { return filepath.Join(c.Dir, "projects") }
func (c *Config) LayoutsDir() string  { return filepath.Join(c.Dir, "layouts") }

func (c *Config) ProjectFile(name string) string {
	return filepath.Join(c.ProjectsDir(), name+".yaml")
}

func (c *Config) LayoutFile(name string) string {
	return filepath.Join(c.LayoutsDir(), name+".yaml")
}

// Projects returns every project in projects/, sorted by name.
func (c *Config) Projects() ([]Project, error) {
	names, err := yamlNames(c.ProjectsDir())
	if err != nil {
		return nil, err
	}
	var out []Project
	for _, n := range names {
		p, err := c.Project(n)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Project loads projects/<name>.yaml. If the project's root has a .ws.yaml,
// that file is used instead.
func (c *Config) Project(name string) (Project, error) {
	p, err := c.readProject(c.ProjectFile(name), name, "")
	if err != nil {
		return p, err
	}
	if local, err := c.readProject(filepath.Join(p.Root, LocalFile), name, p.Root); err == nil {
		return local, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	return p, nil
}

// ForDir returns the project for a directory: its .ws.yaml, a project in
// projects/ rooted there, or a bare project using the default layout.
func (c *Config) ForDir(dir string) (Project, error) {
	dir = filepath.Clean(dir)
	name := SessionName(filepath.Base(dir))
	if p, err := c.readProject(filepath.Join(dir, LocalFile), name, dir); err == nil {
		return p, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return p, err
	}

	projects, err := c.Projects()
	if err != nil {
		return Project{}, err
	}
	for _, p := range projects {
		if p.Root == dir {
			return p, nil
		}
	}
	return Project{Name: name, Root: dir}, nil
}

func (c *Config) readProject(path, name, root string) (Project, error) {
	var p Project
	if err := readYAML(path, &p); err != nil {
		return p, err
	}
	p.File = path
	if p.Name == "" {
		p.Name = name
	}
	p.Name = SessionName(p.Name)
	if p.Root == "" {
		p.Root = root
	}
	if p.Root == "" {
		return p, fmt.Errorf("%s: root is required", path)
	}
	p.Root = c.ExpandHome(p.Root)
	if p.Worktrees != nil && p.Worktrees.Source == "" {
		return p, fmt.Errorf("%s: worktrees.source is required", path)
	}
	return p, nil
}

// Windows returns a project's windows: inline, else its layout, else the
// default layout. override, when set, replaces the project's layout.
func (c *Config) Windows(p Project, override string) ([]layout.Window, error) {
	if override == "" && len(p.Windows) > 0 {
		return p.Windows, nil
	}
	name := override
	if name == "" {
		name = p.Layout
	}
	if name == "" {
		name = c.Settings.DefaultLayout
	}
	f, err := c.Layout(name)
	return f.Windows, err
}

// Layout loads layouts/<name>.yaml. "default" falls back to a single pane
// when there's no file for it.
func (c *Config) Layout(name string) (layout.File, error) {
	if name == "" {
		name = DefaultLayout
	}
	var f layout.File
	err := readYAML(c.LayoutFile(name), &f)
	switch {
	case errors.Is(err, os.ErrNotExist) && name == DefaultLayout:
		return builtinDefault, nil
	case errors.Is(err, os.ErrNotExist):
		return f, fmt.Errorf("layout %q not found in %s", name, c.LayoutsDir())
	case err != nil:
		return f, err
	case len(f.Windows) == 0:
		return f, fmt.Errorf("layout %q has no windows", name)
	}
	return f, nil
}

// Layouts lists layout names in layouts/.
func (c *Config) Layouts() ([]string, error) { return yamlNames(c.LayoutsDir()) }

// ExpandHome turns a leading ~ into the home directory.
func (c *Config) ExpandHome(p string) string {
	if p == "~" {
		return c.Home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(c.Home, p[2:])
	}
	return filepath.Clean(p)
}

// AbbrevHome turns a path under the home directory into ~/...
func (c *Config) AbbrevHome(p string) string {
	if p == c.Home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(p, c.Home+"/"); ok {
		return "~/" + rel
	}
	return p
}

// SessionName makes a name valid for tmux, which rejects "." and ":".
func SessionName(s string) string {
	return strings.NewReplacer(".", "_", ":", "_").Replace(s)
}

// WriteYAML encodes v to path, refusing to overwrite unless force is set.
func WriteYAML(path string, v any, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
	}
	data, err := EncodeYAML(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// EncodeYAML encodes with 2-space indentation.
func EncodeYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), enc.Close()
}

func readYAML(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if data, err = tildeAsHome(data); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// tildeAsHome makes `dir: ~` and `root: ~` mean the home directory. YAML
// reads a bare ~ as null, which would otherwise silently mean "unset".
func tildeAsHome(data []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || doc.Kind == 0 {
		return data, err
	}
	changed := false
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				key, val := n.Content[i], n.Content[i+1]
				if (key.Value == "dir" || key.Value == "root") && val.Tag == "!!null" && val.Value == "~" {
					val.Tag, val.Style, changed = "!!str", yaml.DoubleQuotedStyle, true
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&doc)
	if !changed {
		return data, nil
	}
	return yaml.Marshal(&doc)
}

func yamlNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".yaml"); ok && !e.IsDir() {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}
