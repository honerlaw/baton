package orchestrator

import (
	"testing"

	"github.com/honerlaw/baton/internal/workflow"
)

func TestExtractVerdictValue_JSONBlock(t *testing.T) {
	content := "Some prose before.\n\n```json\n{\"decision\":\"revise_impl\",\"fixes\":[\"a\"]}\n```\n\nMore prose."
	v, err := extractVerdictValue(&workflow.VerdictRule{
		Parser: workflow.VerdictJSONBlock, Field: ".decision",
	}, content)
	if err != nil {
		t.Fatal(err)
	}
	if v != "revise_impl" {
		t.Fatalf("v=%q", v)
	}
}

func TestExtractVerdictValue_Structured(t *testing.T) {
	content := "decision: accept\nnotes: ok\n"
	v, err := extractVerdictValue(&workflow.VerdictRule{
		Parser: workflow.VerdictStructured, Field: ".decision",
	}, content)
	if err != nil {
		t.Fatal(err)
	}
	if v != "accept" {
		t.Fatalf("v=%q", v)
	}
}
