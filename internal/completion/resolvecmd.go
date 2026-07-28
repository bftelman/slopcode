package completion

import (
	"os"
	"os/exec"
	"path/filepath"
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
		candidate := filepath.Join(dir, cmd)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return cmd
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
