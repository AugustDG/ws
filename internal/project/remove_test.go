package project

import (
	"os"
	"slices"
	"testing"
)

func TestRemoveProjectAndLayout(t *testing.T) {
	c := testConfig(t)
	writeFile(t, c.ProjectFile("p"), "root: /x\n")
	writeFile(t, c.LayoutFile("grid"), "windows: [{name: g}]\n")

	if _, err := c.RemoveProject("p"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.ProjectFile("p")); !os.IsNotExist(err) {
		t.Error("project file still there")
	}
	if _, err := c.RemoveProject("p"); err == nil {
		t.Error("removing a missing project should fail")
	}
	if _, err := c.RemoveLayout("grid"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RemoveLayout("grid"); err == nil {
		t.Error("removing a missing layout should fail")
	}
	for _, bad := range []string{"../p", "a/b", "..", ""} {
		if _, err := c.RemoveProject(bad); err == nil {
			t.Errorf("RemoveProject(%q) should be rejected", bad)
		}
	}
}

func TestLayoutUsers(t *testing.T) {
	c := testConfig(t)
	c.Settings.DefaultLayout = "grid"
	writeFile(t, c.ProjectFile("a"), "root: /x\nlayout: grid\n")
	writeFile(t, c.ProjectFile("b"), "root: /x\nlayout: other\n")
	writeFile(t, c.ProjectFile("c"), "root: /x\nlayout: grid\n")

	users, err := c.LayoutUsers("grid")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(users, []string{"config.yaml", "a", "c"}) {
		t.Errorf("users = %v", users)
	}
	if users, _ := c.LayoutUsers("unused"); len(users) != 0 {
		t.Errorf("unused layout has users %v", users)
	}
}
