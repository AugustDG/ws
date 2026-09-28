package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitWritesLoadableConfig(t *testing.T) {
	c := testConfig(t)
	res, err := c.Init(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != 2 || len(res.Skipped) != 0 {
		t.Fatalf("got %+v", res)
	}
	if info, err := os.Stat(c.ProjectsDir()); err != nil || !info.IsDir() {
		t.Error("projects/ not created")
	}

	var s Settings
	if err := readYAML(filepath.Join(c.Dir, "config.yaml"), &s); err != nil || s.DefaultLayout != DefaultLayout {
		t.Errorf("config.yaml: %+v, %v", s, err)
	}
	f, err := c.Layout(DefaultLayout)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Windows) != 1 || f.Windows[0].Name != builtinDefault.Windows[0].Name {
		t.Errorf("default layout differs from the built-in one: %+v", f)
	}
}

func TestInitKeepsExistingFiles(t *testing.T) {
	c := testConfig(t)
	writeFile(t, filepath.Join(c.Dir, "config.yaml"), "default_layout: grid\n")

	res, err := c.Init(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skipped) != 1 || len(res.Written) != 1 {
		t.Fatalf("got %+v", res)
	}
	data, _ := os.ReadFile(filepath.Join(c.Dir, "config.yaml"))
	if string(data) != "default_layout: grid\n" {
		t.Error("existing config.yaml was overwritten")
	}

	if res, _ := c.Init(true); len(res.Written) != 2 {
		t.Errorf("force should rewrite both: %+v", res)
	}
}
