package app

import (
	"reflect"
	"testing"

	"github.com/uvwt/agentdock-protocol/mcpcontract"
)

func TestCanonicalToolDefinitionsMatchSharedContract(t *testing.T) {
	canonicalNames := make([]string, 0, len(mcpcontract.ToolNames()))
	for _, name := range mcpcontract.ToolNames() {
		if name != mcpcontract.ToolAgentDockContext {
			canonicalNames = append(canonicalNames, name)
		}
	}

	definitions := make(map[string]ToolDefinition, len(canonicalNames))
	for _, definition := range ToolDefinitions() {
		if mcpcontract.IsCanonicalTool(definition.Name) {
			definitions[definition.Name] = definition
		}
	}
	if len(definitions) != len(canonicalNames) {
		t.Fatalf("canonical tool count=%d want=%d", len(definitions), len(canonicalNames))
	}

	for _, name := range canonicalNames {
		definition, ok := definitions[name]
		if !ok {
			t.Fatalf("canonical tool %s missing", name)
		}
		wantInput, _ := mcpcontract.InputSchema(name)
		if !reflect.DeepEqual(definition.InputSchema, wantInput) {
			t.Fatalf("%s input schema drifted from shared contract", name)
		}
		wantOutput, _ := mcpcontract.OutputSchema(name)
		if !reflect.DeepEqual(definition.OutputSchema, wantOutput) {
			t.Fatalf("%s output schema drifted from shared contract", name)
		}
		assertToolAnnotationsMatchContract(t, name, definition.Annotations)
	}
}

func TestRuntimeContextReusesSharedContextContract(t *testing.T) {
	var definition *ToolDefinition
	for _, candidate := range ToolDefinitions() {
		if candidate.Name == "runtime_context" {
			copy := candidate
			definition = &copy
			break
		}
	}
	if definition == nil {
		t.Fatal("runtime_context definition missing")
	}

	wantInput, _ := mcpcontract.InputSchema(mcpcontract.ToolAgentDockContext)
	if !reflect.DeepEqual(definition.InputSchema, wantInput) {
		t.Fatal("runtime_context input schema drifted from shared context contract")
	}
	wantOutput := mcpcontract.LocalAgentDockContextOutputSchema()
	if !reflect.DeepEqual(definition.OutputSchema, wantOutput) {
		t.Fatal("runtime_context output schema drifted from shared local context contract")
	}
	assertToolAnnotationsMatchContract(t, mcpcontract.ToolAgentDockContext, definition.Annotations)
}

func assertToolAnnotationsMatchContract(t *testing.T, contractName string, annotations *ToolAnnotations) {
	t.Helper()
	wantAnnotations, _ := mcpcontract.AnnotationContract(contractName)
	if annotations == nil ||
		annotations.ReadOnlyHint != wantAnnotations.ReadOnlyHint ||
		!reflect.DeepEqual(annotations.DestructiveHint, wantAnnotations.DestructiveHint) ||
		!reflect.DeepEqual(annotations.OpenWorldHint, wantAnnotations.OpenWorldHint) {
		t.Fatalf("%s annotations drifted: got=%#v want=%#v", contractName, annotations, wantAnnotations)
	}
	wantIdempotent := wantAnnotations.IdempotentHint != nil && *wantAnnotations.IdempotentHint
	if annotations.IdempotentHint != wantIdempotent {
		t.Fatalf("%s idempotentHint=%v want=%v", contractName, annotations.IdempotentHint, wantIdempotent)
	}
}
