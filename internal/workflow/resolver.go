package workflow

import (
	"bytes"
	"fmt"
	"text/template"
)

// Resolver expands task templates with run-time data.
type Resolver struct {
	Vars     map[string]string
	UserNote string
}

// Resolve renders task against the provided data. artifacts and prev are
// optional; keys map logical artifact name to its current contents.
//
// Templates see a map with keys: vars, artifacts, prev, user_note.
func (r *Resolver) Resolve(task string, artifacts, prev map[string]string) (string, error) {
	t, err := template.New("task").Option("missingkey=zero").Parse(task)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	data := map[string]any{
		"vars":      r.Vars,
		"artifacts": artifacts,
		"prev":      prev,
		"user_note": r.UserNote,
	}
	if data["vars"] == nil {
		data["vars"] = map[string]string{}
	}
	if data["artifacts"] == nil {
		data["artifacts"] = map[string]string{}
	}
	if data["prev"] == nil {
		data["prev"] = map[string]string{}
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute: %w", err)
	}
	return buf.String(), nil
}
