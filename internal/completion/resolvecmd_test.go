package completion

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// goDir returns the directory containing the go tool, so tests can restrict
// PATH to "nothing but go" while still letting resolveCmd shell out to it.
func goDir(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	return filepath.Dir(p)
}

// writeStub creates an executable stub for the tool named name and returns the
// path resolveCmd should produce for it.
//
// The ".exe" on Windows is not cosmetic: a file without one of PATHEXT's
// extensions is not executable there, so exec.LookPath would never find it and
// an extensionless stub would be testing a scenario that cannot occur. Real
// `go install` writes "gopls.exe", and these tests have to match that.
func writeStub(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func TestResolveCmdPrefersPATH(t *testing.T) {
	dir := t.TempDir()
	stub := writeStub(t, dir, "faketool")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+goDir(t))

	got := resolveCmd("faketool")
	if got != stub {
		t.Fatalf("resolveCmd = %q, want %q", got, stub)
	}
}

func TestResolveCmdFallsBackToGOBIN(t *testing.T) {
	empty := t.TempDir()
	gobin := t.TempDir()
	stub := writeStub(t, gobin, "faketool2")

	t.Setenv("PATH", empty+string(os.PathListSeparator)+goDir(t))
	t.Setenv("GOBIN", gobin)

	got := resolveCmd("faketool2")
	if got != stub {
		t.Fatalf("resolveCmd = %q, want %q", got, stub)
	}
}

func TestResolveCmdFallsBackToGOPATHBin(t *testing.T) {
	empty := t.TempDir()
	gopath := t.TempDir()
	binDir := filepath.Join(gopath, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := writeStub(t, binDir, "faketool3")

	t.Setenv("PATH", empty+string(os.PathListSeparator)+goDir(t))
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", gopath)

	got := resolveCmd("faketool3")
	if got != stub {
		t.Fatalf("resolveCmd = %q, want %q", got, stub)
	}
}

func TestResolveCmdReturnsBareNameWhenNotFound(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty+string(os.PathListSeparator)+goDir(t))
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", empty)

	got := resolveCmd("doesnotexist12345")
	if got != "doesnotexist12345" {
		t.Fatalf("resolveCmd = %q, want bare name unchanged", got)
	}
}
