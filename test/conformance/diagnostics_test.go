package conformance

import (
	"strings"
	"testing"
	"time"
)

// breakingEdit is text appended to a fixture file that its language server
// must report. Syntax errors are used where a server only reports semantic
// problems after a build or save (metals, rust-analyzer's cargo check), so the
// test measures arno's diagnostics path rather than a server's build model.
var breakingEdit = map[string]string{
	"go":         "\nfunc arnoBroken() { undefinedThing() }\n",
	"typescript": "\nconst arnoBroken: number = undefinedThing;\n",
	"javascript": "\nconst = ;\n",
	"python":     "\narno_broken = undefined_thing\n",
	"ruby":       "\ndef arno_broken(\n",
	"rust":       "\nfn arno_broken( {\n",
	"java":       "\nclass ArnoBroken { void f() { undefinedThing(); } }\n",
	"scala":      "\nobject ArnoBroken { def f( = 1 }\n",
}

// TestEditDiagnostics proves the claim every edit response now makes: it names
// what checked the file, and a real error comes back in the response.
//
// A response that says "checked: X" with no diagnostic line for a file that is
// plainly broken would be worse than the old silence, because it reads as a
// clean bill of health. So the assertion is on both halves.
func TestEditDiagnostics(t *testing.T) {
	binary := arnoBinary(t)

	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			if reason, ok := serverUsable(tc.server); !ok {
				t.Skipf("%s: %s", tc.server, reason)
			}
			edit, ok := breakingEdit[tc.name]
			if !ok {
				t.Fatalf("no breaking edit defined for %s", tc.name)
			}

			root := fixtureCopy(t, tc.dir)
			session := newSession(t, binary, root)
			defer session.close()

			// Start the server and let it index before the edit under test,
			// the same way an agent's first read would.
			session.call(t, "arno.references", map[string]any{"path": tc.file, "symbolName": tc.symbol})

			// A server still importing the build (metals: bloopInstall, then
			// connect, then index) says so rather than answering. That is
			// the correct response, so retry until it is ready — the
			// assertion below is about the answer once there is one.
			var out string
			deadline := time.Now().Add(serverDeadline * 2)
			for {
				started := time.Now()
				out = session.call(t, "arno.insert", map[string]any{"path": tc.file, "text": edit})
				t.Logf("%s edit response after %v:\n%s", tc.name, time.Since(started).Round(time.Millisecond), out)
				notReady := strings.Contains(out, "is still working") || strings.Contains(out, "has not compiled this change yet")
				if !notReady || time.Now().After(deadline) {
					break
				}
				time.Sleep(5 * time.Second)
			}

			header := strings.SplitN(out, "\n", 2)[0]
			if !strings.Contains(header, "checked: ") {
				t.Fatalf("%s: edit response does not name a checker:\n%s", tc.name, out)
			}
			if strings.Contains(out, "not checked: ") {
				t.Fatalf("%s: server is installed but the file went unchecked:\n%s", tc.name, out)
			}
			if !hasDiagnosticLine(out) {
				t.Fatalf("%s: a broken file was reported as checked with no diagnostics:\n%s", tc.name, out)
			}
		})
	}
}

func hasDiagnosticLine(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		for _, level := range []string{"error ", "warning ", "info "} {
			if strings.HasPrefix(line, level) {
				return true
			}
		}
	}
	return false
}
