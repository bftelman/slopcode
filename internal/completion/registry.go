package completion

import "strings"

// Registry resolves a completion Provider for a file extension and root dir.
// Factory returns (nil, nil) when the extension is unsupported.
type Registry struct {
	Factory func(ext, root string) (Provider, error)
}

// stripFileScheme returns the path for a file:// URI. On Windows a leading
// "file:///C:/x" becomes "C:/x". Handles both three-slash URIs (file:///path)
// and four-slash URIs (file:////path) from file URI construction.
func stripFileScheme(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, "file://")
	if !ok {
		return "", false
	}
	// Trim all leading slashes, preserving the path structure
	for len(rest) > 0 && rest[0] == '/' {
		rest = rest[1:]
	}
	// Check for Windows drive letter (e.g., "C:" after removing leading slashes)
	if len(rest) >= 2 && rest[1] == ':' {
		return rest, true
	}
	// Unix path: ensure it starts with /
	return "/" + rest, true
}
