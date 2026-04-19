package workflow

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// rawWorkflow is the YAML-facing type.
type rawWorkflow struct {
	Name         string       `yaml:"name"`
	Version      string       `yaml:"version"`
	Description  string       `yaml:"description"`
	DefaultModel string       `yaml:"default_model"`
	MaxReentries *int         `yaml:"max_reentries"`
	Variables    []rawVarDecl `yaml:"variables"`
	Stages       []rawStage   `yaml:"stages"`
}

type rawVarDecl struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    *bool  `yaml:"required"`
	Default     string `yaml:"default"`
}

type rawStage struct {
	ID           string           `yaml:"id"`
	Name         string           `yaml:"name"`
	Parallel     bool             `yaml:"parallel"`
	Members      []rawStageMember `yaml:"members"`
	Persona      string           `yaml:"persona"`
	Model        string           `yaml:"model"`
	Task         string           `yaml:"task"`
	Inputs       []string         `yaml:"inputs"`
	Artifact     string           `yaml:"artifact"`
	MaxReentries int              `yaml:"max_reentries"`
	OnFail       string           `yaml:"on_fail"`
	Verdict      *rawVerdict      `yaml:"verdict"`
}

type rawStageMember struct {
	ID       string   `yaml:"id"`
	Persona  string   `yaml:"persona"`
	Model    string   `yaml:"model"`
	Task     string   `yaml:"task"`
	Inputs   []string `yaml:"inputs"`
	Artifact string   `yaml:"artifact"`
}

type rawVerdict struct {
	Parser string            `yaml:"parser"`
	Field  string            `yaml:"field"`
	Routes map[string]string `yaml:"routes"`
}

// LoadFile reads and parses a workflow YAML file.
func LoadFile(path string) (*Workflow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	w, err := Load(b)
	if err != nil {
		return nil, err
	}
	w.SourcePath = path
	return w, nil
}

// Load parses workflow bytes. The result's SourcePath is empty; callers
// who want it should set it (LoadFile does).
func Load(b []byte) (*Workflow, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	var raw rawWorkflow
	if err := root.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	// Build a map from id -> line for diagnostic position recovery.
	lines := gatherLines(&root)

	w := &Workflow{
		Name:         raw.Name,
		Version:      raw.Version,
		Description:  raw.Description,
		DefaultModel: raw.DefaultModel,
		MaxReentries: 1,
		SourceBytes:  append([]byte(nil), b...),
	}
	if raw.MaxReentries != nil {
		w.MaxReentries = *raw.MaxReentries
	}
	for _, v := range raw.Variables {
		required := true
		if v.Required != nil {
			required = *v.Required
		}
		w.Variables = append(w.Variables, VarDecl{
			Name:        v.Name,
			Description: v.Description,
			Required:    required,
			Default:     v.Default,
		})
	}
	for _, rs := range raw.Stages {
		s := Stage{
			ID:           rs.ID,
			Name:         stringOr(rs.Name, rs.ID),
			Parallel:     rs.Parallel,
			Persona:      rs.Persona,
			Model:        rs.Model,
			Task:         rs.Task,
			Inputs:       rs.Inputs,
			Artifact:     rs.Artifact,
			MaxReentries: rs.MaxReentries,
			OnFail:       FailPolicy(stringOr(rs.OnFail, string(FailHalt))),
			Line:         lines[rs.ID],
		}
		for _, rm := range rs.Members {
			s.Members = append(s.Members, StageMember{
				ID:       rm.ID,
				Persona:  rm.Persona,
				Model:    rm.Model,
				Task:     rm.Task,
				Inputs:   rm.Inputs,
				Artifact: rm.Artifact,
				Line:     lines[rs.ID+"/"+rm.ID],
			})
		}
		if rs.Verdict != nil {
			s.Verdict = &VerdictRule{
				Parser: VerdictParser(rs.Verdict.Parser),
				Field:  rs.Verdict.Field,
				Routes: rs.Verdict.Routes,
			}
		}
		w.Stages = append(w.Stages, s)
	}
	return w, nil
}

// gatherLines walks the YAML AST to produce a map of stage-id (and
// stage-id "/" member-id) to source line number. It's best-effort —
// validation errors gracefully fall back to line 0.
func gatherLines(root *yaml.Node) map[string]int {
	out := map[string]int{}
	if root == nil || len(root.Content) == 0 {
		return out
	}
	// root is a document node whose content[0] is the top map.
	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		return out
	}
	// Find the "stages:" sequence.
	for i := 0; i+1 < len(top.Content); i += 2 {
		key := top.Content[i]
		val := top.Content[i+1]
		if key.Value != "stages" || val.Kind != yaml.SequenceNode {
			continue
		}
		for _, stageNode := range val.Content {
			if stageNode.Kind != yaml.MappingNode {
				continue
			}
			var id string
			var membersNode *yaml.Node
			for j := 0; j+1 < len(stageNode.Content); j += 2 {
				k := stageNode.Content[j]
				v := stageNode.Content[j+1]
				switch k.Value {
				case "id":
					id = v.Value
				case "members":
					membersNode = v
				}
			}
			if id != "" {
				out[id] = stageNode.Line
			}
			if id != "" && membersNode != nil && membersNode.Kind == yaml.SequenceNode {
				for _, m := range membersNode.Content {
					if m.Kind != yaml.MappingNode {
						continue
					}
					var mid string
					for j := 0; j+1 < len(m.Content); j += 2 {
						if m.Content[j].Value == "id" {
							mid = m.Content[j+1].Value
							break
						}
					}
					if mid != "" {
						out[id+"/"+mid] = m.Line
					}
				}
			}
		}
	}
	return out
}

func stringOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
