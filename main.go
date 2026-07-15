// Command slopcode is a minimal fullscreen terminal text editor.
package main

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"

	"github.com/bftelman/slopcode/internal/buffer"
	"github.com/bftelman/slopcode/internal/editor"
	"github.com/bftelman/slopcode/internal/fileio"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: slopcode <filename>")
		os.Exit(2)
	}
	path := os.Args[1]

	lines, err := fileio.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open %s: %v\n", path, err)
		os.Exit(1)
	}

	s, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot init screen: %v\n", err)
		os.Exit(1)
	}
	if err := s.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot init screen: %v\n", err)
		os.Exit(1)
	}
	defer s.Fini()

	b := buffer.New(lines)
	editor.New(s, b, path).Run()
}
