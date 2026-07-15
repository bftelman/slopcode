// Package highlight turns source text into per-line styled runes using chroma.
package highlight

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/gdamore/tcell/v2"
)

// StyledRune is a single rune paired with its display style.
type StyledRune struct {
	R     rune
	Style tcell.Style
}

// Highlight tokenises text (language chosen by filename) and returns per-line
// styled runes. The concatenation of all runes equals the input exactly.
func Highlight(text, filename, styleName string) [][]StyledRune {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}

	tokens, err := chroma.Tokenise(lexer, nil, text)
	if err != nil {
		return splitPlain(text)
	}

	lines := [][]StyledRune{{}}
	for _, tok := range tokens {
		st := toTcellStyle(style.Get(tok.Type))
		for _, r := range tok.Value {
			if r == '\n' {
				lines = append(lines, []StyledRune{})
				continue
			}
			last := len(lines) - 1
			lines[last] = append(lines[last], StyledRune{R: r, Style: st})
		}
	}
	return lines
}

func splitPlain(text string) [][]StyledRune {
	lines := [][]StyledRune{{}}
	for _, r := range text {
		if r == '\n' {
			lines = append(lines, []StyledRune{})
			continue
		}
		last := len(lines) - 1
		lines[last] = append(lines[last], StyledRune{R: r, Style: tcell.StyleDefault})
	}
	return lines
}

func toTcellStyle(e chroma.StyleEntry) tcell.Style {
	s := tcell.StyleDefault
	if e.Colour.IsSet() {
		s = s.Foreground(tcell.NewRGBColor(
			int32(e.Colour.Red()), int32(e.Colour.Green()), int32(e.Colour.Blue())))
	}
	if e.Bold == chroma.Yes {
		s = s.Bold(true)
	}
	if e.Italic == chroma.Yes {
		s = s.Italic(true)
	}
	if e.Underline == chroma.Yes {
		s = s.Underline(true)
	}
	return s
}
