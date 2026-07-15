// Package filebrowser lists a directory for navigation. No UI dependencies.
package filebrowser

import (
	"os"
	"path/filepath"
	"sort"
)

// Entry is one item in the listing.
type Entry struct {
	Name  string
	IsDir bool
}

// Browser holds a directory listing and the current selection.
type Browser struct {
	dir     string
	entries []Entry
	sel     int
}

// Open reads dir and returns a Browser positioned at the first entry.
func Open(dir string) (*Browser, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	b := &Browser{dir: filepath.Clean(abs)}
	if err := b.load(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Browser) load() error {
	items, err := os.ReadDir(b.dir)
	if err != nil {
		return err
	}
	var dirs, files []Entry
	for _, it := range items {
		if it.IsDir() {
			dirs = append(dirs, Entry{Name: it.Name(), IsDir: true})
		} else {
			files = append(files, Entry{Name: it.Name(), IsDir: false})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	entries := []Entry{{Name: "..", IsDir: true}}
	entries = append(entries, dirs...)
	entries = append(entries, files...)
	b.entries = entries
	b.sel = 0
	return nil
}

// Entries returns the current directory listing.
func (b *Browser) Entries() []Entry { return b.entries }

// Dir returns the current directory path.
func (b *Browser) Dir() string { return b.dir }

// SelIndex returns the selected entry index.
func (b *Browser) SelIndex() int { return b.sel }

// Selected returns the currently selected entry.
func (b *Browser) Selected() Entry { return b.entries[b.sel] }

// MoveUp moves the selection up, clamped.
func (b *Browser) MoveUp() {
	if b.sel > 0 {
		b.sel--
	}
}

// MoveDown moves the selection down, clamped.
func (b *Browser) MoveDown() {
	if b.sel < len(b.entries)-1 {
		b.sel++
	}
}

// Enter descends into the selected directory (returns isDir=true) or returns a
// file's full path (isDir=false).
func (b *Browser) Enter() (string, bool, error) {
	e := b.entries[b.sel]
	if e.IsDir {
		b.dir = filepath.Clean(filepath.Join(b.dir, e.Name))
		return "", true, b.load()
	}
	return filepath.Join(b.dir, e.Name), false, nil
}
