// Package fileio loads and saves editor files. It has no UI dependencies.
package fileio

import (
	"os"
	"strings"
)

// Load reads a file into lines. A missing file yields a single blank line.
// A trailing carriage return on each line is stripped (Windows CRLF).
func Load(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{""}, nil
		}
		return nil, err
	}
	text := strings.TrimSuffix(string(data), "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	if len(lines) == 0 {
		return []string{""}, nil
	}
	return lines, nil
}

// Save writes lines joined by newlines, with a trailing newline.
func Save(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}
