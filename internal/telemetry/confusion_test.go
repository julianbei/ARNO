package telemetry

import (
	"reflect"
	"testing"
)

func TestTargetOfHashesTheMostSpecificArgument(t *testing.T) {
	a := TargetOf(map[string]interface{}{"path": "a.go", "query": "x"})
	b := TargetOf(map[string]interface{}{"path": "a.go"})
	if a == "" || a != b {
		t.Fatalf("path should decide the target when present: %q vs %q", a, b)
	}
	if TargetOf(map[string]interface{}{"path": "b.go"}) == a {
		t.Fatal("different paths must hash differently")
	}
	if got := TargetOf(map[string]interface{}{"path": "a.go"}); got == "a.go" {
		t.Fatal("the target must not be stored in the clear")
	}
	if TargetOf(map[string]interface{}{"reset": true}) != "" {
		t.Fatal("a call that names nothing has no target")
	}
}

func TestAnalyzeConfusionFindsSwitchesRetriesAndUnusedTools(t *testing.T) {
	fileA := TargetOf(map[string]interface{}{"path": "a.go"})
	fileB := TargetOf(map[string]interface{}{"path": "b.go"})
	records := []Record{
		{Tool: "arno.read_range", Target: fileA, Outcome: OK},
		{Tool: "arno.outline", Target: fileA, Outcome: OK},      // switch
		{Tool: "arno.replace_text", Target: fileA, Outcome: OK}, // read then edit: not a switch
		{Tool: "arno.read_symbol", Target: fileB, Outcome: NotFound},
		{Tool: "arno.read_symbol", Target: fileB, Outcome: OK}, // retry
		{Tool: "arno.outline", Target: fileA, Outcome: OK},     // different target: not a switch
	}

	report := AnalyzeConfusion(records, []string{"arno.read_range", "arno.outline", "arno.rename", "arno.find"})

	wantSwitches := []ToolSwitch{{From: "arno.read_range", To: "arno.outline", Count: 1}}
	if !reflect.DeepEqual(report.Switches, wantSwitches) {
		t.Errorf("switches: got %+v, want %+v", report.Switches, wantSwitches)
	}
	wantRetries := []ToolRetry{{Tool: "arno.read_symbol", After: NotFound, Count: 1}}
	if !reflect.DeepEqual(report.Retries, wantRetries) {
		t.Errorf("retries: got %+v, want %+v", report.Retries, wantRetries)
	}
	if want := []string{"arno.find", "arno.rename"}; !reflect.DeepEqual(report.NeverCalled, want) {
		t.Errorf("never called: got %v, want %v", report.NeverCalled, want)
	}
}
