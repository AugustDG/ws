package project

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// RemoveProject deletes projects/<name>.yaml and returns its path. A repo's
// own .ws.yaml is never touched.
func (c *Config) RemoveProject(name string) (string, error) {
	return remove(c.ProjectFile(name), "project", name)
}

// RemoveLayout deletes layouts/<name>.yaml and returns its path.
func (c *Config) RemoveLayout(name string) (string, error) {
	return remove(c.LayoutFile(name), "layout", name)
}

func remove(path, kind, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return path, fmt.Errorf("invalid %s name %q", kind, name)
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, fmt.Errorf("no %s %q", kind, name)
	}
	return path, err
}

// LayoutUsers lists what refers to a layout by name: projects using it and,
// as "config.yaml", the default_layout setting.
func (c *Config) LayoutUsers(name string) ([]string, error) {
	var users []string
	if c.Settings.DefaultLayout == name {
		users = append(users, "config.yaml")
	}
	projects, err := c.Projects()
	if err != nil {
		return users, err
	}
	for _, p := range projects {
		if p.Layout == name {
			users = append(users, p.Name)
		}
	}
	return users, nil
}
