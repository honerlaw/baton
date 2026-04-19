package persona

import "os"

// DirLoader returns an FSLoader rooted at dir on the real filesystem.
// The loader looks for persona files directly under dir.
func DirLoader(dir, label string) *FSLoader {
	return &FSLoader{FS: os.DirFS(dir), Name: label}
}
