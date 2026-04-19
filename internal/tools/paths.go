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
