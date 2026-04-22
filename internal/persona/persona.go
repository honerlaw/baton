// Package persona parses Claude-format persona files (.md with YAML
// frontmatter) and exposes a chained loader with precedence.
package persona

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Persona is a parsed persona file.
type Persona struct {
	Name        string
	Description string
	Model       string
	Tools       []string
	Body        string
	SourcePath  string
}

// Loader resolves persona names to parsed Personas.
type Loader interface {
	Load(name string) (*Persona, error)
	List() ([]*Persona, error)
}

// FSLoader loads personas from an fs.FS rooted at a directory.
// Filenames must be <name>.md; the frontmatter name field must match.
type FSLoader struct {
	FS   fs.FS
	Dir  string // directory inside FS (e.g., ".baton/personas"); empty for root
	Name string // diagnostic label (e.g., "project" or "embedded")
}

// Load finds and parses the named persona.
func (l *FSLoader) Load(name string) (*Persona, error) {
	path := name + ".md"
	if l.Dir != "" {
		path = filepath.Join(l.Dir, path)
	}
	b, err := fs.ReadFile(l.FS, path)
	if err != nil {
		return nil, err
	}
	p, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	p.SourcePath = path
	if p.Name != name {
		return nil, fmt.Errorf("%s: frontmatter name %q does not match filename %q", path, p.Name, name)
	}
	return p, nil
}

// List enumerates all personas visible to this loader.
func (l *FSLoader) List() ([]*Persona, error) {
	dir := l.Dir
	if dir == "" {
		dir = "."
	}
	entries, err := fs.ReadDir(l.FS, dir)
	if err != nil {
		return nil, err
	}
	var out []*Persona
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		p, err := l.Load(name)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ChainLoader tries loaders in order, returning the first match.
type ChainLoader struct {
	Loaders []Loader
}

// Load returns the first loader's persona for name.
func (l *ChainLoader) Load(name string) (*Persona, error) {
	var errs []string
	for _, loader := range l.Loaders {
		p, err := loader.Load(name)
		if err == nil {
			return p, nil
		}
		errs = append(errs, err.Error())
	}
	return nil, fmt.Errorf("persona %q not found: %s", name, strings.Join(errs, "; "))
}

// List returns the union of all loaders' personas, with earlier loaders
// taking precedence on name conflict.
func (l *ChainLoader) List() ([]*Persona, error) {
	seen := map[string]bool{}
	var out []*Persona
	for _, loader := range l.Loaders {
		ps, err := loader.List()
		if err != nil {
			continue
		}
		for _, p := range ps {
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			out = append(out, p)
		}
	}
	return out, nil
}

// Parse reads a persona Markdown file's bytes and returns the parsed Persona.
// Format: frontmatter delimited by lines containing only "---" at the start
// of the file, YAML inside, Markdown body after the closing delimiter.
func Parse(b []byte) (*Persona, error) {
	content := string(b)
	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("missing frontmatter opening ---")
	}
	rest := strings.TrimPrefix(content, "---")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, fmt.Errorf("missing frontmatter closing ---")
	}
	fm := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")

	var raw struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Model       string `yaml:"model"`
		Tools       any    `yaml:"tools"`
	}
	if err := yaml.Unmarshal([]byte(fm), &raw); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	if raw.Name == "" {
		return nil, fmt.Errorf("frontmatter.name is required")
	}
	if raw.Description == "" {
		return nil, fmt.Errorf("frontmatter.description is required")
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("body is empty")
	}
	tools, err := coerceTools(raw.Tools)
	if err != nil {
		return nil, err
	}
	return &Persona{
		Name:        raw.Name,
		Description: raw.Description,
		Model:       raw.Model,
		Tools:       tools,
		Body:        body,
	}, nil
}

func coerceTools(v any) ([]string, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, nil
		}
		var out []string
		for _, part := range strings.Split(t, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	case []any:
		var out []string
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("tools[] must be strings, got %T", item)
			}
			out = append(out, strings.TrimSpace(s))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("tools must be a string or sequence, got %T", v)
	}
}
