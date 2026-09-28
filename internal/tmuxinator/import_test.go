package tmuxinator

import (
	"slices"
	"testing"
)

const home = "/home/me"

func TestParse(t *testing.T) {
	src := `
name: demo
root: ~/code/demo
pre_window: nvm use
on_project_start: docker compose up -d
windows:
  - editor: nvim
  - servers:
      root: api
      layout: main-vertical
      panes:
        - cd ~/code/demo/web
        - - bun install
          - bun dev
        - logs:
            - tail -f log
        -
  - grid:
      layout: 5e1a,200x50,0,0{100x50,0,0,1,99x50,101,0,2}
      panes:
        - cd ../other
        - htop
`
	p, err := Parse([]byte(src), home)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.Root != home+"/code/demo" || p.OnStart != "docker compose up -d" {
		t.Errorf("project = %+v", p)
	}
	if len(p.Windows) != 3 {
		t.Fatalf("got %d windows", len(p.Windows))
	}

	editor := p.Windows[0]
	if editor.Cell == nil || !slices.Equal(editor.Panes[0].Cmds, []string{"nvm use", "nvim"}) {
		t.Errorf("editor = %+v", editor)
	}

	servers := p.Windows[1]
	if servers.Preset != "main-vertical" || len(servers.Panes) != 4 {
		t.Fatalf("servers = %+v", servers)
	}
	if servers.Panes[0].Dir != home+"/code/demo/web" || !slices.Equal(servers.Panes[0].Cmds, []string{"nvm use"}) {
		t.Errorf("cd pane = %+v", servers.Panes[0])
	}
	if servers.Panes[1].Dir != home+"/code/demo/api" || len(servers.Panes[1].Cmds) != 3 {
		t.Errorf("list pane = %+v", servers.Panes[1])
	}
	if servers.Panes[2].Cmds[1] != "tail -f log" {
		t.Errorf("named pane = %+v", servers.Panes[2])
	}

	grid := p.Windows[2]
	if grid.Cell == nil || len(grid.Cell.Children) != 2 || grid.Panes[0].Dir != home+"/code/other" {
		t.Errorf("grid = %+v", grid)
	}
}

func TestParseLayoutMismatchFallsBack(t *testing.T) {
	src := `
root: /x
windows:
  - w:
      layout: 5e1a,200x50,0,0{100x50,0,0,1,99x50,101,0,2}
      panes: [a, b, c]
`
	p, err := Parse([]byte(src), home)
	if err != nil {
		t.Fatal(err)
	}
	if p.Windows[0].Preset != "tiled" || len(p.Warnings) != 1 {
		t.Errorf("got %+v, warnings %v", p.Windows[0], p.Warnings)
	}
}

func TestParseRejectsERB(t *testing.T) {
	if _, err := Parse([]byte("root: <%= ENV['X'] %>"), home); err == nil {
		t.Error("expected an error")
	}
}

func TestPaneCdForms(t *testing.T) {
	tests := []struct {
		cmd, dir string
		cmds     []string
	}{
		{"cd api && npm run dev", "/r/api", []string{"npm run dev"}},
		{`cd "my dir"`, "/r/my dir", nil},
		{"cd ~/x", home + "/x", nil},
		{"cd $DIR", "/r", []string{"cd $DIR"}},
		{"cd -", "/r", []string{"cd -"}},
		{"cd a; ls", "/r", []string{"cd a; ls"}},
	}
	for _, tt := range tests {
		p := pane([]string{tt.cmd}, "/r", home, nil)
		if p.Dir != tt.dir || !slices.Equal(p.Cmds, tt.cmds) {
			t.Errorf("%q: got dir %q cmds %q", tt.cmd, p.Dir, p.Cmds)
		}
	}
}
