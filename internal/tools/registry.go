// Package tools defines the Tool interface, a Registry, and the built-in
// tool implementations used by personas.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/honerlaw/baton/internal/openrouter"
)

// Tool is implemented by every executable tool.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// Result is returned from dispatch.
type Result struct {
	CallID  string
	Name    string
	Content string
	Error   string
}

// Call is an invocation requested by the model.
type Call struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Registry holds registered tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

// Register adds t. Returns an error if a tool with the same name exists.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[t.Name()]; ok {
		return fmt.Errorf("tool already registered: %s", t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

// MustRegister panics on error; useful for built-ins at startup.
func (r *Registry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// Has reports whether a tool with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// Names returns the registered tool names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for k := range r.tools {
		names = append(names, k)
	}
	return names
}

// Dispatch executes a call, enforcing the persona's allowlist.
// If allowed is empty, all tools are permitted.
func (r *Registry) Dispatch(ctx context.Context, call Call, allowed []string) Result {
	if len(allowed) > 0 && !contains(allowed, call.Name) {
		return Result{
			CallID: call.ID, Name: call.Name,
			Error: fmt.Sprintf("tool %q not permitted for this persona", call.Name),
		}
	}
	r.mu.RLock()
	t, ok := r.tools[call.Name]
	r.mu.RUnlock()
	if !ok {
		return Result{CallID: call.ID, Name: call.Name, Error: fmt.Sprintf("unknown tool %q", call.Name)}
	}
	out, err := t.Execute(ctx, call.Arguments)
	if err != nil {
		return Result{CallID: call.ID, Name: call.Name, Error: err.Error(), Content: out}
	}
	return Result{CallID: call.ID, Name: call.Name, Content: out}
}

// OpenRouterDecls returns the tool declarations for the OpenRouter API,
// filtered to the given allowlist (empty => all tools).
func (r *Registry) OpenRouterDecls(allowed []string) []openrouter.ToolDecl {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		if len(allowed) > 0 && !contains(allowed, n) {
			continue
		}
		names = append(names, n)
	}
	decls := make([]openrouter.ToolDecl, 0, len(names))
	for _, n := range names {
		t := r.tools[n]
		decls = append(decls, openrouter.ToolDecl{
			Type: "function",
			Function: openrouter.ToolFuncDecl{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return decls
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// RegisterBuiltins registers all built-in tools against r. workDir bounds
// the paths that file tools may touch (combined with an optional runRoot).
func RegisterBuiltins(r *Registry, workDir, runRoot string) error {
	for _, t := range []Tool{
		&ReadFile{WorkDir: workDir, RunRoot: runRoot},
		&WriteFile{WorkDir: workDir, RunRoot: runRoot},
		&ListFiles{WorkDir: workDir},
		&Bash{WorkDir: workDir},
		&Search{WorkDir: workDir},
	} {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// ErrPathOutsideSafe is returned when a tool is asked to touch a path that
// is neither inside the run root nor inside the working directory.
var ErrPathOutsideSafe = errors.New("path outside safe directories")
