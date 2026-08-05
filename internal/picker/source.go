// Package picker supplies candidate lists and ranks them against a fuzzy query.
// It is UI-free: it must not import tcell, buffer, or render. Buffer contents
// reach it as a plain []string.
package picker

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Candidate is one selectable row. Which destination is meaningful is decided by
// the Source that produced it: file sources set Path, line sources set Row.
type Candidate struct {
	Text string // matched against the query, and shown in the list
	Path string // file to open; "" for line candidates
	Row  int    // line to jump to; ignored for file candidates
}

// Source produces a picker's candidate list, once per open.
type Source interface {
	Title() string
	Candidates() ([]Candidate, error)
}

// Lister returns root-relative, forward-slashed file paths under root. It is the
// seam Files uses internally, exported so tests can supply canned output rather
// than depending on which binaries exist on the machine.
type Lister func(root string) ([]string, error)

// listTimeout bounds each external lister. Exceeding it falls through to the
// next strategy rather than failing: a slow or wedged rg must not hang a picker.
const listTimeout = 5 * time.Second

// walkLimit caps the fallback walk, so a picker opened at a filesystem root
// cannot run unbounded.
const walkLimit = 200000

// skipDirs are never descended into by the WalkDir fallback. rg and fd get this
// for free from .gitignore; the fallback has no such knowledge.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "target": true, ".venv": true, "__pycache__": true,
}

type fileSource struct {
	root string
	list Lister
}

// Files lists files under root using DefaultLister.
func Files(root string) Source { return fileSource{root: root, list: DefaultLister} }

// FilesWith is Files with an injected Lister.
func FilesWith(root string, l Lister) Source { return fileSource{root: root, list: l} }

func (f fileSource) Title() string { return "Files · " + filepath.Base(f.root) }

func (f fileSource) Candidates() ([]Candidate, error) {
	paths, err := f.list(f.root)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(paths))
	for _, p := range paths {
		out = append(out, Candidate{
			Text: p,
			Path: filepath.Join(f.root, filepath.FromSlash(p)),
		})
	}
	return out, nil
}

type lineSource struct{ lines []string }

// Lines wraps a snapshot of buffer lines. Blank lines are skipped: they cannot
// be matched and would only pad the list.
func Lines(lines []string) Source {
	cp := make([]string, len(lines))
	copy(cp, lines)
	return lineSource{lines: cp}
}

func (l lineSource) Title() string { return "Lines in buffer" }

func (l lineSource) Candidates() ([]Candidate, error) {
	out := make([]Candidate, 0, len(l.lines))
	for i, ln := range l.lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		// The line number is part of Text so a query can target it, and so the
		// list stays readable without a separate column.
		out = append(out, Candidate{
			Text: fmt.Sprintf("%d: %s", i+1, strings.TrimRight(ln, " \t")),
			Row:  i,
		})
	}
	return out, nil
}

// GitRoot walks up from start looking for a .git entry and returns that
// directory. It falls back to start when there is no repository above it, so a
// file outside any repo still gets a usable picker root.
func GitRoot(start string) string {
	dir := start
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return start
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs { // reached the volume root
			return start
		}
		abs = parent
	}
}

// DefaultLister prefers ripgrep, then fd, then a plain directory walk. A missing
// binary, a non-zero exit, or a timeout falls through to the next strategy: as
// with gopls, the absence of an external tool is not an error.
func DefaultLister(root string) ([]string, error) {
	for _, try := range []func(string) ([]string, error){listRg, listFd} {
		if paths, err := try(root); err == nil && len(paths) > 0 {
			return paths, nil
		}
	}
	return listWalk(root)
}

func listRg(root string) ([]string, error) {
	return runLister(root, "rg", "--files", "--hidden", "--glob", "!.git")
}

func listFd(root string) ([]string, error) {
	return runLister(root, "fd", "--type", "f", "--hidden", "--exclude", ".git")
}

// runLister runs name inside root and reads root-relative paths from its stdout.
func runLister(root, name string, args ...string) ([]string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return ParseListerOutput(string(out)), nil
}

// ParseListerOutput turns a lister's raw stdout into clean relative paths. It is
// exported so tests can exercise the parsing against canned output without
// needing rg or fd installed.
func ParseListerOutput(out string) []string {
	var paths []string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if p := normalizePath(sc.Text()); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// normalizePath trims a lister's output line into a clean relative slash path.
func normalizePath(line string) string {
	p := strings.TrimSpace(line)
	p = strings.TrimPrefix(p, "./")
	p = filepath.ToSlash(p)
	if p == "" || p == "." {
		return ""
	}
	return p
}

// listWalk is the no-external-tools fallback.
func listWalk(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory must not abort the whole listing.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(paths) >= walkLimit {
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if s := normalizePath(rel); s != "" {
			paths = append(paths, s)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}
