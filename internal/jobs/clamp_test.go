package jobs

import (
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/diagnostics"
)

func TestClampLeavesSmallOutputUntouched(t *testing.T) {
	output := "ok  \tgithub.com/julianbei/arno/internal/code\t0.248s\n"
	clamped, omitted := clampRawOutput(output)
	if clamped != output {
		t.Fatalf("expected small output to pass through unchanged, got %q", clamped)
	}
	if omitted != 0 {
		t.Fatalf("expected 0 omitted bytes, got %d", omitted)
	}
}

func TestClampKeepsBothEndsOfLargeOutput(t *testing.T) {
	// Both ends matter: a compiler reports the first error first, `go test`
	// prints FAIL last. Keeping only one end loses the decisive part for one
	// of them.
	var builder strings.Builder
	builder.WriteString("FIRST-LINE-MARKER\n")
	for i := 0; i < 4000; i++ {
		builder.WriteString("filler line that exists only to exceed the budget\n")
	}
	builder.WriteString("LAST-LINE-MARKER\n")
	output := builder.String()

	clamped, omitted := clampRawOutput(output)

	if len(clamped) > maxRawOutputBytes+400 {
		t.Fatalf("expected clamped output near the budget, got %d bytes", len(clamped))
	}
	if omitted <= 0 {
		t.Fatalf("expected omitted bytes to be reported, got %d", omitted)
	}
	if !strings.Contains(clamped, "FIRST-LINE-MARKER") {
		t.Fatalf("expected the head to survive clamping")
	}
	if !strings.Contains(clamped, "LAST-LINE-MARKER") {
		t.Fatalf("expected the tail to survive clamping")
	}
	// Truncation must be visible. A reader who cannot see that bytes are
	// missing will read the remainder as complete.
	if !strings.Contains(clamped, "bytes omitted") {
		t.Fatalf("expected an explicit omission marker, got %q", clamped[:200])
	}
}

func TestClampCutsOnLineBoundaries(t *testing.T) {
	var builder strings.Builder
	for i := 0; i < 2000; i++ {
		builder.WriteString("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	}
	clamped, _ := clampRawOutput(builder.String())

	head := clamped[:strings.Index(clamped, "\n\n... ")]
	for _, line := range strings.Split(head, "\n") {
		if line != "" && len(line) != 40 {
			t.Fatalf("expected whole lines only, got a %d-char line %q", len(line), line)
		}
	}
}

func TestClampReportsAccurateOmittedCount(t *testing.T) {
	var builder strings.Builder
	for i := 0; i < 3000; i++ {
		builder.WriteString("0123456789\n")
	}
	original := builder.String()

	clamped, omitted := clampRawOutput(original)
	marker := strings.Index(clamped, "\n\n... ")
	head := clamped[:marker]
	tail := clamped[strings.Index(clamped[marker+2:], "...\n\n")+marker+2+len("...\n\n"):]

	// head + omitted + tail must account for every original byte.
	if len(head)+omitted+len(tail) != len(original) {
		t.Fatalf("byte accounting is wrong: head=%d omitted=%d tail=%d original=%d",
			len(head), omitted, len(tail), len(original))
	}
}

func TestCompleteWithOutputSummarizesFullOutputNotClampedOutput(t *testing.T) {
	// Pins the ordering: summarize first, clamp second. The two currently
	// coincide because DecisiveSummary reads trailing lines and the clamp
	// preserves the tail — but the ordering is the contract, and it is what
	// keeps a future summarizer that looks anywhere but the tail from
	// silently losing the clamped middle.
	var builder strings.Builder
	for i := 0; i < 2000; i++ {
		builder.WriteString("filler output line with nothing interesting in it\n")
	}
	builder.WriteString("main.go:42:3: undefined: decisiveMiddleSymbol\n")
	for i := 0; i < 2000; i++ {
		builder.WriteString("filler output line with nothing interesting in it\n")
	}
	full := builder.String()

	r := NewRunner(nil)
	id := r.Start("typecheck")
	r.CompleteWithOutput(id, full)

	output, ok := r.Output(id)
	if !ok {
		t.Fatalf("expected the job to be found")
	}
	if output.OmittedBytes <= 0 {
		t.Fatalf("expected the raw output to have been clamped, got %d omitted", output.OmittedBytes)
	}
	if want := diagnostics.DecisiveSummary(full); output.Summary != want {
		t.Fatalf("expected the summary of the FULL output\n got: %q\nwant: %q", output.Summary, want)
	}
}

func TestOutputReportsZeroOmittedForShortJobs(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("tests")
	r.CompleteWithOutput(id, "ok\tinternal/code\t0.2s\n")

	output, ok := r.Output(id)
	if !ok {
		t.Fatalf("expected the job to be found")
	}
	if output.OmittedBytes != 0 {
		t.Fatalf("expected 0 omitted bytes for a short job, got %d", output.OmittedBytes)
	}
}
func TestFullOutputKeepsWhatTheClampDrops(t *testing.T) {
	r := NewRunner(nil)
	half := strings.Repeat("0123456789abcdef0123456789abcdef012345678\n", 500)
	big := half + "MIDDLE-MARKER\n" + half
	r.CompleteWithResult("job-1", big, true)

	output, _ := r.Output("job-1")
	if output.OmittedBytes == 0 || strings.Contains(output.Raw, "MIDDLE-MARKER") {
		t.Fatal("the output verdicts read should stay clamped")
	}
	full, ok := r.FullOutput("job-1")
	if !ok || full != big {
		t.Fatalf("expected the whole output kept for paging, got %d of %d bytes", len(full), len(big))
	}
}
