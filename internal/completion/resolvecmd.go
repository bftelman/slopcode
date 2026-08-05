package completion

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// resolveCmd finds an absolute path for cmd, falling back to the locations
// `go install` places tools in when cmd is not on $PATH. GUI-launched
// editors often inherit a minimal $PATH that omits $GOBIN/$GOPATH/bin, even
// though tools like gopls were installed there (e.g. by an IDE extension).
// Returns cmd unchanged if it cannot be resolved; the caller's exec attempt
// then fails with the original, informative "not found" error.
func resolveCmd(cmd string) string {
	if path, err := exec.LookPath(cmd); err == nil {
		return path
	}
	for _, dir := range goToolDirs() {
		if dir == "" {
			continue
		}
		for _, name := range execNames(cmd) {
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	return cmd
}

// execNames returns the filenames to look for when resolving cmd inside a tool
// directory, most likely first.
//
// On Windows this matters: a file is only executable if it carries one of
// PATHEXT's extensions, and `go install` writes "gopls.exe" — so stat-ing a bare
// "gopls" finds nothing, which silently disabled this entire fallback on the one
// platform it was most needed. Extensions are lowercased because that is what
// `go install` writes, and Windows filenames are case-insensitive anyway, so a
// lowercase probe still matches an upper-case name on disk.
func execNames(cmd string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(cmd) != "" {
		return []string{cmd}
	}
	exts := os.Getenv("PATHEXT")
	if exts == "" {
		exts = ".com;.exe;.bat;.cmd"
	}
	var names []string
	for _, ext := range strings.Split(exts, ";") {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		names = append(names, cmd+ext)
	}
	return names
}

// goToolDirs lists directories `go install` may have placed tools in, most
// specific first: $GOBIN, then $GOPATH/bin (or ~/go/bin if GOPATH is unset).
func goToolDirs() []string {
	dirs := []string{os.Getenv("GOBIN")}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		if out, err := exec.Command("go", "env", "GOPATH").Output(); err == nil {
			gopath = strings.TrimSpace(string(out))
		}
	}
	if gopath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			gopath = filepath.Join(home, "go")
		}
	}
	if gopath != "" {
		dirs = append(dirs, filepath.Join(gopath, "bin"))
	}
	return dirs
}
