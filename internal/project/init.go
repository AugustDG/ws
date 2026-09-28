package project

import (
	"errors"
	"os"
	"path/filepath"
)

// starterFiles is what Init writes, relative to the config dir. The default
// layout matches builtinDefault, so writing it changes nothing until edited.
var starterFiles = []struct{ path, content string }{
	{"config.yaml", `# ws settings.

# Layout for directories without a project file and for projects that don't
# name one. Refers to layouts/<name>.yaml.
default_layout: default
`},
	{filepath.Join("layouts", DefaultLayout+".yaml"), `# The layout used when nothing else is named: one window with one pane.
#
# Windows split into columns and rows sized in percent, and any node can set
# for_each: worktree to repeat once per worktree. For example:
#
#   windows:
#     - name: code
#       columns:
#         - for_each: worktree
#           rows:
#             - size: 70%
#               cmd: nvim
#             - {}
windows:
  - name: main
`},
}

// InitResult says what Init did with each starter file.
type InitResult struct {
	Written []string
	Skipped []string // already existed
}

// Init creates the config dir with starter files. Existing files are left
// alone unless force is set.
func (c *Config) Init(force bool) (InitResult, error) {
	var res InitResult
	if err := os.MkdirAll(c.ProjectsDir(), 0o755); err != nil {
		return res, err
	}
	for _, f := range starterFiles {
		path := filepath.Join(c.Dir, f.path)
		if _, err := os.Stat(path); err == nil && !force {
			res.Skipped = append(res.Skipped, path)
			continue
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return res, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(path, []byte(f.content), 0o644); err != nil {
			return res, err
		}
		res.Written = append(res.Written, path)
	}
	return res, nil
}
