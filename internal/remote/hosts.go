package remote

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Hosts lists the concrete host aliases in an ssh config file and the
// files it includes, for completion. Patterns (*, ?, !) are skipped.
// Missing or unreadable files contribute nothing.
func Hosts(configFile string) []string {
	seen := map[string]bool{}
	var out []string
	visited := map[string]bool{}
	var read func(path string)
	read = func(path string) {
		if visited[path] {
			return
		}
		visited[path] = true
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			key, rest, ok := directive(sc.Text())
			if !ok {
				continue
			}
			switch key {
			case "host":
				for _, h := range strings.Fields(rest) {
					if !strings.ContainsAny(h, "*?!") && !seen[h] {
						seen[h] = true
						out = append(out, h)
					}
				}
			case "include":
				for _, pat := range strings.Fields(rest) {
					matches, _ := filepath.Glob(includePath(configFile, pat))
					for _, m := range matches {
						read(m)
					}
				}
			}
		}
	}
	read(configFile)
	return out
}

// directive splits "Key value" or "Key=value", lowercasing the key.
func directive(line string) (key, rest string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return "", "", false
	}
	return strings.ToLower(line[:i]), strings.TrimLeft(line[i:], " \t="), true
}

// includePath resolves an Include argument the way ssh does: ~ is the
// home directory and relative paths are relative to the config's
// directory.
func includePath(configFile, p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(filepath.Dir(configFile), p)
}
