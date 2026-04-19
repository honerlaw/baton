// Package render provides non-TUI consumers of the event bus:
// an NDJSON file logger and a plaintext terminal renderer.
package render

import (
	"encoding/json"
	"io"
	"os"

	"github.com/honerlaw/baton/internal/event"
)

// NDJSONLogger writes every event as one JSON object per line.
type NDJSONLogger struct {
	w io.WriteCloser
}

// NewNDJSONLogger opens path for append-write. Caller must Close.
func NewNDJSONLogger(path string) (*NDJSONLogger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &NDJSONLogger{w: f}, nil
}

// Run subscribes to bus and writes every event until the channel closes.
func (l *NDJSONLogger) Run(ch <-chan event.Event) error {
	enc := json.NewEncoder(l.w)
	for ev := range ch {
		rec := map[string]any{"kind": ev.Kind(), "payload": ev}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

// Close flushes and closes the underlying file.
func (l *NDJSONLogger) Close() error { return l.w.Close() }
