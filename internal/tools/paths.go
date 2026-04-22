package tools

import (
	"path/filepath"
	"strings"
)

// resolveSafe resolves path relative to workDir and returns the absolute
// path. It returns ErrPathOutsideSafe if the result is outside both
// workDir and runRoot. runRoot may be empty (no run-dir check).
func resolveSafe(path, workDir, runRoot string) (string, error) {
	p := path
	if !filepath.IsAbs(p) {
		p = filepath.Join(workDir, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if under(abs, workDir) {
		return abs, nil
	}
	if runRoot != "" && under(abs, runRoot) {
		return abs, nil
	}
	return "", ErrPathOutsideSafe
}

func under(abs, root string) bool {
	if root == "" {
		return false
	}
	r, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	return abs == r || strings.HasPrefix(abs, r+string(filepath.Separator))
}

// resolvedUnder reports whether path, after resolving symlinks, is inside
// root (also symlink-resolved). Used to defend tools that enumerate the
// filesystem against symlink-based escapes. Returns false on
// EvalSymlinks errors (e.g. broken links or missing files) — the caller
// should treat those as "not safe to include."
//
// Both sides are symlink-resolved because some OSes (notably macOS,
// where /var is a symlink to /private/var) would otherwise produce a
// resolved path that does not textually share a prefix with a root that
// was passed in un-resolved.
func resolvedUnder(path, root string) bool {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	return under(resolvedPath, resolvedRoot)
}
