package remote

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// StampFile marks a config dir on a host as written by ws ssh setup. It
// holds the Bundle hash, so an unchanged dir isn't sent again.
const StampFile = ".ws-sync"

// Bundle is a set of files under Root to copy to a host. Paths limits it
// to those entries (files or dirs) of Root; empty means all of Root.
// .git (dir or submodule file) and stamp files are left out.
type Bundle struct {
	Root  string
	Paths []string
}

type entry struct {
	rel  string
	abs  string
	info fs.FileInfo
	link string
}

func (b Bundle) entries() ([]entry, error) {
	root, err := filepath.EvalSymlinks(b.Root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	starts := []string{root}
	if len(b.Paths) > 0 {
		starts = nil
		for _, p := range b.Paths {
			starts = append(starts, filepath.Join(root, p))
		}
	}
	var out []entry
	for _, start := range starts {
		err := filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) && path == start {
					return nil
				}
				return err
			}
			if d.Name() == ".git" {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil // a submodule's gitdir pointer
			}
			if path == root || d.Name() == StampFile {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			e := entry{rel: filepath.ToSlash(rel), abs: path, info: info}
			if info.Mode()&fs.ModeSymlink != 0 {
				if e.link, err = os.Readlink(path); err != nil {
					return err
				}
			}
			out = append(out, e)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	slices.SortFunc(out, func(a, b entry) int {
		if a.rel < b.rel {
			return -1
		}
		if a.rel > b.rel {
			return 1
		}
		return 0
	})
	return out, nil
}

// Hash identifies the bundle's names, modes, link targets and contents.
// An empty bundle hashes to "".
func (b Bundle) Hash() (string, error) {
	entries, err := b.entries()
	if err != nil || len(entries) == 0 {
		return "", err
	}
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%o\x00%s\x00", e.rel, e.info.Mode(), e.link)
		if e.info.Mode().IsRegular() {
			if err := copyFile(h, e.abs); err != nil {
				return "", err
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFile writes the contents of path to w.
func copyFile(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// WriteTar writes the bundle as a gzipped tar with paths relative to Root.
func (b Bundle) WriteTar(w io.Writer) error {
	entries, err := b.entries()
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr, err := tar.FileInfoHeader(e.info, e.link)
		if err != nil {
			return err
		}
		hdr.Name = e.rel
		hdr.Uname, hdr.Gname, hdr.Uid, hdr.Gid = "", "", 0, 0
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if e.info.Mode().IsRegular() {
			if err := copyFile(tw, e.abs); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// FileHash is the hex sha256 of a file.
func FileHash(path string) (string, error) {
	h := sha256.New()
	if err := copyFile(h, path); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
