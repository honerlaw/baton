package workflow

import (
	"strings"
	"testing"

	"github.com/honerlaw/baton/internal/persona"
	"github.com/honerlaw/baton/internal/tools"
)

const goodYAML = `
name: t
version: 1.0.0
default_model: m
max_reentries: 1
variables:
  - name: q
    required: true
stages:
  - id: s1
    persona: p1
    artifact: a.md
    task: "{{ .vars.q }}"
  - id: s2
    persona: p1
    inputs: [a.md]
    artifact: b.md
    task: "hi"
`

func TestValidator_OK(t *testing.T) {
	w := mustLoad(t, goodYAML)
	v := &Validator{Personas: &stubLoader{"p1"}, Tools: tools.NewRegistry()}
	r := v.Validate(w)
	if !r.OK() {
		t.Fatalf("expected ok, got: %s", r.Error())
	}
}

func TestValidator_DetectsUnknownArtifact(t *testing.T) {
	w := mustLoad(t, `
name: t
version: 1.0.0
default_model: m
stages:
  - id: s1
    persona: p1
    artifact: a.md
    task: "hi"
  - id: s2
    persona: p1
    inputs: [missing.md]
    artifact: b.md
    task: "hi"
`)
	v := &Validator{Personas: &stubLoader{"p1"}, Tools: tools.NewRegistry()}
	r := v.Validate(w)
	if r.OK() {
		t.Fatal("expected failure")
	}
	if !strings.Contains(r.Error(), `unknown artifact "missing.md"`) {
		t.Fatalf("missing expected error: %s", r.Error())
	}
}

func TestValidator_DetectsUnknownVariable(t *testing.T) {
	w := mustLoad(t, `
name: t
version: 1.0.0
default_model: m
stages:
  - id: s1
    persona: p1
    artifact: a.md
    task: "{{ .vars.x }}"
`)
	v := &Validator{Personas: &stubLoader{"p1"}, Tools: tools.NewRegistry()}
	r := v.Validate(w)
	if r.OK() {
		t.Fatal("expected failure")
	}
	if !strings.Contains(r.Error(), "undeclared variable .vars.x") {
		t.Fatalf("missing expected error: %s", r.Error())
	}
}

func TestValidator_DetectsVerdictWithoutReentries(t *testing.T) {
	w := mustLoad(t, `
name: t
version: 1.0.0
default_model: m
max_reentries: 0
stages:
  - id: s1
    persona: p1
    artifact: a.md
    task: "hi"
  - id: s2
    persona: p1
    inputs: [a.md]
    artifact: b.md
    task: "hi"
    verdict:
      parser: json_block
      field: .decision
      routes:
        revise: s1
        accept: ""
`)
	v := &Validator{Personas: &stubLoader{"p1"}, Tools: tools.NewRegistry()}
	r := v.Validate(w)
	if r.OK() {
		t.Fatal("expected failure")
	}
	if !strings.Contains(r.Error(), "max_reentries=0") {
		t.Fatalf("missing expected error: %s", r.Error())
	}
}

func TestResolver_ExpandsVarsAndArtifacts(t *testing.T) {
	r := &Resolver{Vars: map[string]string{"name": "world"}}
	out, err := r.Resolve("hi {{ .vars.name }} - {{ .artifacts.foo }}",
		map[string]string{"foo": "bar"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hi world - bar" {
		t.Fatalf("got %q", out)
	}
}

func mustLoad(t *testing.T, src string) *Workflow {
	t.Helper()
	w, err := Load([]byte(src))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return w
}

// stubLoader resolves any name in the whitelist to a trivial persona.
type stubLoader struct {
	ok string
}

func (s *stubLoader) Load(name string) (*persona.Persona, error) {
	if name == s.ok {
		return &persona.Persona{Name: name, Description: "x", Body: "body"}, nil
	}
	return nil, fmtErr("unknown persona %q", name)
}

func (s *stubLoader) List() ([]*persona.Persona, error) {
	return []*persona.Persona{{Name: s.ok, Description: "x", Body: "body"}}, nil
}

type fmtError struct{ s string }

func (e *fmtError) Error() string { return e.s }

func fmtErr(format string, a ...any) error {
	return &fmtError{s: formatString(format, a...)}
}

func formatString(format string, a ...any) string {
	// fmt.Sprintf without the import to avoid cycle-ish noise in tests
	b := strings.Builder{}
	i := 0
	ai := 0
	for i < len(format) {
		if format[i] == '%' && i+1 < len(format) {
			verb := format[i+1]
			if verb == 'q' && ai < len(a) {
				b.WriteByte('"')
				b.WriteString(toString(a[ai]))
				b.WriteByte('"')
				ai++
				i += 2
				continue
			}
			if verb == 's' && ai < len(a) {
				b.WriteString(toString(a[ai]))
				ai++
				i += 2
				continue
			}
		}
		b.WriteByte(format[i])
		i++
	}
	return b.String()
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case error:
		return x.Error()
	default:
		return "?"
	}
}
