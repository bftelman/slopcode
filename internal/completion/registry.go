package completion

import "strings"

// Registry resolves a completion Provider for a file extension and root dir.
// Factory returns (nil, nil) when the extension is unsupported.
type Registry struct {
	Factory func(ext, root string) (Provider, error)
}

// stripFileScheme returns the path for a file:// URI. On Windows a leading
// "file:///C:/x" becomes "C:/x".
func stripFileScheme(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, "file://")
	if !ok {
		return "", false
	}
	rest = strings.TrimPrefix(rest, "/")
	if len(rest) >= 2 && rest[1] == ':' { // Windows drive
		return rest, true
	}
	return "/" + rest, true
}
