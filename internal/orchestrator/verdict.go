package orchestrator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/honerlaw/baton/internal/workflow"
)

// extractVerdictValue parses artifact content according to rule.Parser and
// returns the value at rule.Field (dot-path JSONPath-lite).
func extractVerdictValue(rule *workflow.VerdictRule, content string) (string, error) {
	switch rule.Parser {
	case workflow.VerdictJSONBlock:
		obj, err := extractJSONBlock(content)
		if err != nil {
			return "", err
		}
		return lookupField(obj, rule.Field)
	case workflow.VerdictStructured:
		var obj any
		if err := yaml.Unmarshal([]byte(content), &obj); err != nil {
			// Try JSON if YAML fails (YAML is a superset; most JSON parses as YAML).
			if err2 := json.Unmarshal([]byte(content), &obj); err2 != nil {
				return "", fmt.Errorf("parse structured: %w", err)
			}
		}
		return lookupField(obj, rule.Field)
	default:
		return "", fmt.Errorf("unknown parser %q", rule.Parser)
	}
}

var jsonBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// extractJSONBlock finds the first fenced JSON block in content and parses it.
// Falls back to the entire content if no fence is present.
func extractJSONBlock(content string) (any, error) {
	m := jsonBlockRe.FindStringSubmatch(content)
	var raw string
	if len(m) >= 2 {
		raw = m[1]
	} else {
		raw = strings.TrimSpace(content)
	}
	var obj any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, fmt.Errorf("json block: %w", err)
	}
	return obj, nil
}

// lookupField resolves a dot-prefixed field path (".a.b") against obj.
func lookupField(obj any, path string) (string, error) {
	p := strings.TrimPrefix(path, ".")
	if p == "" {
		return stringify(obj)
	}
	cur := obj
	for _, seg := range strings.Split(p, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("field %q: not an object at %q", path, seg)
		}
		cur, ok = m[seg]
		if !ok {
			return "", fmt.Errorf("field %q: missing key %q", path, seg)
		}
	}
	return stringify(cur)
}

func stringify(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case bool:
		if t {
			return "true", nil
		}
		return "false", nil
	case nil:
		return "", nil
	default:
		return fmt.Sprintf("%v", t), nil
	}
}
