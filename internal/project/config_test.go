package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AugustDG/ws/internal/layout"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{Dir: t.TempDir(), Home: "/home/me"}
}

func TestProjectTildeMeansHome(t *testing.T) {
	c := testConfig(t)
	writeFile(t, c.ProjectFile("p"), `
root: ~
windows:
  - name: a
    dir: ~
`)
	p, err := c.Project("p")
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != "/home/me" || p.Windows[0].Dir != "~" {
		t.Errorf("root %q, window dir %q", p.Root, p.Windows[0].Dir)
	}
}

func TestProjectRejectsUnknownFields(t *testing.T) {
	c := testConfig(t)
	writeFile(t, c.ProjectFile("p"), "root: /x\nlayuot: grid\n")
	if _, err := c.Project("p"); err == nil {
		t.Error("expected an error for a misspelled field")
	}
}

func TestLocalFileWins(t *testing.T) {
	c := testConfig(t)
	root := t.TempDir()
	writeFile(t, c.ProjectFile("p"), "root: "+root+"\nlayout: central\n")
	writeFile(t, filepath.Join(root, LocalFile), "layout: local\n")

	p, err := c.Project("p")
	if err != nil {
		t.Fatal(err)
	}
	if p.Layout != "local" || p.Root != root || p.Name != "p" {
		t.Errorf("got %+v", p)
	}

	byDir, err := c.ForDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if byDir.Layout != "local" {
		t.Errorf("ForDir got %+v", byDir)
	}
}

func TestForDirFallsBackToDefault(t *testing.T) {
	c := testConfig(t)
	p, err := c.ForDir("/code/my.app")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "my_app" || p.Root != "/code/my.app" {
		t.Errorf("got %+v", p)
	}
	w, err := c.Windows(p, "")
	if err != nil || len(w) != 1 {
		t.Errorf("default layout: %v, %v", w, err)
	}
}

func TestWindowsPrecedence(t *testing.T) {
	c := testConfig(t)
	writeFile(t, c.LayoutFile("grid"), "windows: [{name: g}]\n")
	c.Settings.DefaultLayout = "grid"

	if w, _ := c.Windows(Project{}, ""); w[0].Name != "g" {
		t.Error("settings default_layout not used")
	}
	if _, err := c.Windows(Project{Layout: "missing"}, ""); err == nil {
		t.Error("expected an error for a missing layout")
	}
	inline := Project{Windows: []layout.Window{{Name: "inline"}}}
	if w, _ := c.Windows(inline, ""); w[0].Name != "inline" {
		t.Error("inline windows not preferred")
	}
	if w, _ := c.Windows(inline, "grid"); w[0].Name != "g" {
		t.Error("override not applied")
	}
}
