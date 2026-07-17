package editor

// identChar reports whether r is part of an identifier ([A-Za-z0-9_]).
func identChar(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// wordStart returns the byte index where the identifier ending at col begins.
// col is a byte offset into line. The scan stops at the first non-identifier
// byte (ASCII-correct; see the R4 limitation).
func wordStart(line string, col int) int {
	i := col
	for i > 0 && identChar(rune(line[i-1])) {
		i--
	}
	return i
}

// shouldTrigger reports whether typing r should request completions.
func shouldTrigger(r rune) bool {
	return identChar(r) || r == '.'
}
