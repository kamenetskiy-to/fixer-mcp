package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// Declared write scopes and their lease/fence bookkeeping are fully retired
// from the active MCP surface. These tests derive the real advertised JSON
// schemas for the task, fork, planned-wave, wave and Hands tool families and
// assert that no property resurrects the retired mechanism.

func mustInferSchema[T any](t *testing.T, label string) *jsonschema.Schema {
	t.Helper()
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		t.Fatalf("infer %s schema: %v", label, err)
	}
	return schema
}

func assertSchemaRetiresWriteScopeSurface(t *testing.T, label string, schema *jsonschema.Schema) {
	t.Helper()
	bannedNameParts := []string{"scope", "lease", "fence"}
	var walk func(path string, current *jsonschema.Schema)
	walk = func(path string, current *jsonschema.Schema) {
		if current == nil {
			return
		}
		for name, property := range current.Properties {
			lowered := strings.ToLower(name)
			for _, banned := range bannedNameParts {
				if strings.Contains(lowered, banned) {
					t.Fatalf("%s: property %q at %s must not advertise the retired %s surface", label, name, path, banned)
				}
			}
			walk(path+"."+name, property)
		}
		for name, definition := range current.Defs {
			walk(path+"#"+name, definition)
		}
		if current.Items != nil {
			walk(path+"[]", current.Items)
		}
	}
	walk("$", schema)

	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal %s schema: %v", label, err)
	}
	lowered := strings.ToLower(string(encoded))
	for _, banned := range []string{"declared_write_scope", "declared write scope", "write_scope", "write scope", "scope lease", "scope_lease", "write lease", "write_lease", "write fence", "write_fence"} {
		if strings.Contains(lowered, banned) {
			t.Fatalf("%s: schema text must not advertise %q", label, banned)
		}
	}
}

func TestTaskAndForkToolSchemasAdvertiseNoWriteScope(t *testing.T) {
	cases := []struct {
		label  string
		schema *jsonschema.Schema
	}{
		{"GetPendingTasksInput", mustInferSchema[GetPendingTasksInput](t, "GetPendingTasksInput")},
		{"GetPendingTasksOutput", mustInferSchema[GetPendingTasksOutput](t, "GetPendingTasksOutput")},
		{"CheckoutTaskInput", mustInferSchema[CheckoutTaskInput](t, "CheckoutTaskInput")},
		{"CheckoutTaskOutput", mustInferSchema[CheckoutTaskOutput](t, "CheckoutTaskOutput")},
		{"CreateTaskInput", mustInferSchema[CreateTaskInput](t, "CreateTaskInput")},
		{"CreateTaskOutput", mustInferSchema[CreateTaskOutput](t, "CreateTaskOutput")},
		{"CompleteTaskInput", mustInferSchema[CompleteTaskInput](t, "CompleteTaskInput")},
		{"CompleteTaskOutput", mustInferSchema[CompleteTaskOutput](t, "CompleteTaskOutput")},
		{"UpdateTaskInput", mustInferSchema[UpdateTaskInput](t, "UpdateTaskInput")},
		{"UpdateTaskOutput", mustInferSchema[UpdateTaskOutput](t, "UpdateTaskOutput")},
		{"GetAllSessionsInput", mustInferSchema[GetAllSessionsInput](t, "GetAllSessionsInput")},
		{"GetAllSessionsOutput", mustInferSchema[GetAllSessionsOutput](t, "GetAllSessionsOutput")},
		{"SetSessionStatusInput", mustInferSchema[SetSessionStatusInput](t, "SetSessionStatusInput")},
		{"SetSessionStatusOutput", mustInferSchema[SetSessionStatusOutput](t, "SetSessionStatusOutput")},
		{"ForkRepairSessionFromInput", mustInferSchema[ForkRepairSessionFromInput](t, "ForkRepairSessionFromInput")},
		{"ForkRepairSessionFromOutput", mustInferSchema[ForkRepairSessionFromOutput](t, "ForkRepairSessionFromOutput")},
		{"VerifySessionCleanupClaimsInput", mustInferSchema[VerifySessionCleanupClaimsInput](t, "VerifySessionCleanupClaimsInput")},
		{"VerifySessionCleanupClaimsOutput", mustInferSchema[VerifySessionCleanupClaimsOutput](t, "VerifySessionCleanupClaimsOutput")},
		{"GetSessionInput", mustInferSchema[GetSessionInput](t, "GetSessionInput")},
		{"GetSessionOutput", mustInferSchema[GetSessionOutput](t, "GetSessionOutput")},
	}
	for _, tc := range cases {
		assertSchemaRetiresWriteScopeSurface(t, tc.label, tc.schema)
	}
}

func TestPlannedAndLiveWaveToolSchemasAdvertiseNoWriteScope(t *testing.T) {
	cases := []struct {
		label  string
		schema *jsonschema.Schema
	}{
		{"PlannedWaveTaskInput", mustInferSchema[PlannedWaveTaskInput](t, "PlannedWaveTaskInput")},
		{"CreatePlannedNetrunnerWaveInput", mustInferSchema[CreatePlannedNetrunnerWaveInput](t, "CreatePlannedNetrunnerWaveInput")},
		{"CreatePlannedNetrunnerWaveOutput", mustInferSchema[CreatePlannedNetrunnerWaveOutput](t, "CreatePlannedNetrunnerWaveOutput")},
		{"GetPlannedNetrunnerWaveInput", mustInferSchema[GetPlannedNetrunnerWaveInput](t, "GetPlannedNetrunnerWaveInput")},
		{"GetPlannedNetrunnerWaveOutput", mustInferSchema[GetPlannedNetrunnerWaveOutput](t, "GetPlannedNetrunnerWaveOutput")},
		{"InitializePlannedNetrunnerWaveInput", mustInferSchema[InitializePlannedNetrunnerWaveInput](t, "InitializePlannedNetrunnerWaveInput")},
		{"InitializePlannedNetrunnerWaveOutput", mustInferSchema[InitializePlannedNetrunnerWaveOutput](t, "InitializePlannedNetrunnerWaveOutput")},
		{"CreateNetrunnerWaveInput", mustInferSchema[CreateNetrunnerWaveInput](t, "CreateNetrunnerWaveInput")},
		{"CreateNetrunnerWaveOutput", mustInferSchema[CreateNetrunnerWaveOutput](t, "CreateNetrunnerWaveOutput")},
		{"GetNetrunnerWaveInput", mustInferSchema[GetNetrunnerWaveInput](t, "GetNetrunnerWaveInput")},
		{"GetNetrunnerWaveOutput", mustInferSchema[GetNetrunnerWaveOutput](t, "GetNetrunnerWaveOutput")},
		{"LaunchNetrunnerWaveInput", mustInferSchema[LaunchNetrunnerWaveInput](t, "LaunchNetrunnerWaveInput")},
		{"LaunchNetrunnerWaveOutput", mustInferSchema[LaunchNetrunnerWaveOutput](t, "LaunchNetrunnerWaveOutput")},
		{"WaitForNetrunnerWaveInput", mustInferSchema[WaitForNetrunnerWaveInput](t, "WaitForNetrunnerWaveInput")},
		{"WaitForNetrunnerWaveOutput", mustInferSchema[WaitForNetrunnerWaveOutput](t, "WaitForNetrunnerWaveOutput")},
		{"CleanupNetrunnerWaveInput", mustInferSchema[CleanupNetrunnerWaveInput](t, "CleanupNetrunnerWaveInput")},
		{"CleanupNetrunnerWaveOutput", mustInferSchema[CleanupNetrunnerWaveOutput](t, "CleanupNetrunnerWaveOutput")},
	}
	for _, tc := range cases {
		assertSchemaRetiresWriteScopeSurface(t, tc.label, tc.schema)
	}
}

func TestHandsToolSchemasAdvertiseNoWriteScope(t *testing.T) {
	cases := []struct {
		label  string
		schema *jsonschema.Schema
	}{
		{"GetHandsStateInput", mustInferSchema[GetHandsStateInput](t, "GetHandsStateInput")},
		{"GetHandsStateOutput", mustInferSchema[GetHandsStateOutput](t, "GetHandsStateOutput")},
		{"ListHandsInstructionsInput", mustInferSchema[ListHandsInstructionsInput](t, "ListHandsInstructionsInput")},
		{"ListHandsInstructionsOutput", mustInferSchema[ListHandsInstructionsOutput](t, "ListHandsInstructionsOutput")},
		{"GetHandsInstructionInput", mustInferSchema[GetHandsInstructionInput](t, "GetHandsInstructionInput")},
		{"GetHandsInstructionOutput", mustInferSchema[GetHandsInstructionOutput](t, "GetHandsInstructionOutput")},
		{"SubmitHandsInstructionInput", mustInferSchema[SubmitHandsInstructionInput](t, "SubmitHandsInstructionInput")},
		{"SubmitHandsInstructionOutput", mustInferSchema[SubmitHandsInstructionOutput](t, "SubmitHandsInstructionOutput")},
		{"CancelHandsInstructionInput", mustInferSchema[CancelHandsInstructionInput](t, "CancelHandsInstructionInput")},
		{"ReviewHandsInstructionInput", mustInferSchema[ReviewHandsInstructionInput](t, "ReviewHandsInstructionInput")},
		{"HandsCommandOutput", mustInferSchema[HandsCommandOutput](t, "HandsCommandOutput")},
		{"WaitHandsInstructionInput", mustInferSchema[WaitHandsInstructionInput](t, "WaitHandsInstructionInput")},
		{"WaitHandsInstructionOutput", mustInferSchema[WaitHandsInstructionOutput](t, "WaitHandsInstructionOutput")},
	}
	for _, tc := range cases {
		assertSchemaRetiresWriteScopeSurface(t, tc.label, tc.schema)
	}
}
