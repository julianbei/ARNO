package bench

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/render"
	"github.com/julianbei/jade/internal/transport/internalapi"
)

// API is the subset of the internal server the scenarios exercise. Declared
// as an interface so the benchmark cannot quietly grow a dependency on
// server internals.
type API interface {
	Outline(protocol.OutlineRequest) (protocol.InspectResponse, error)
	ReadSymbol(protocol.ReadSymbolRequest) (protocol.InspectResponse, error)
	ReadRange(protocol.ReadRangeRequest) (protocol.InspectResponse, error)
	Search(protocol.SearchRequest) (protocol.SearchResponse, error)
	References(protocol.ReferencesRequest) (protocol.ReferencesResponse, error)
	WorkspaceTree(protocol.WorkspaceTreeRequest) (protocol.WorkspaceTreeResponse, error)
	Changes() protocol.ChangesResponse
}

var _ API = (*internalapi.Server)(nil)

// DefaultScenarios are questions an agent actually asks while working in a
// repository, each answered both ways.
//
// Fairness rules, because a benchmark whose arms answer different questions
// measures nothing:
//   - both arms must produce enough for the agent to answer the question;
//   - the shell arm uses the command a competent agent would actually reach
//     for, not a deliberately clumsy one;
//   - jade's arm is measured as its response is actually serialized to the
//     agent over MCP (JSON), not as some hypothetical trimmed form.
func DefaultScenarios(api API, root string) []Scenario {
	return []Scenario{
		{
			Name:     "outline one file",
			Question: "What does internal/code/nudge.go declare?",
			Jade: jadeArm(func() (interface{}, error) {
				return api.Outline(protocol.OutlineRequest{Path: "internal/code/nudge.go"})
			}),
			Shell: shellArm(root, `grep -n '^func \|^type \|^var \|^const ' internal/code/nudge.go`),
		},
		{
			Name:     "read one symbol",
			Question: "What is the body of cleanNudgeQuery?",
			Jade: jadeArm(func() (interface{}, error) {
				return api.ReadSymbol(protocol.ReadSymbolRequest{
					Path:       "internal/code/nudge.go",
					SymbolName: "cleanNudgeQuery",
				})
			}),
			// The realistic shell equivalent: find it, then read around it.
			Shell: shellArm(root, `grep -n 'func cleanNudgeQuery' internal/code/nudge.go && sed -n '141,165p' internal/code/nudge.go`),
		},
		{
			Name:     "find a symbol repo-wide",
			Question: "Where is shouldSkipPath defined?",
			Jade: jadeArm(func() (interface{}, error) {
				return api.Search(protocol.SearchRequest{Query: "shouldSkipPath", Limit: 5})
			}),
			Shell: shellArm(root, `grep -rn 'func shouldSkipPath' --include='*.go' .`),
		},
		{
			Name:     "who calls a symbol",
			Question: "What calls cleanNudgeQuery?",
			Jade: jadeArm(func() (interface{}, error) {
				return api.References(protocol.ReferencesRequest{
					Path:       "internal/code/nudge.go",
					SymbolName: "cleanNudgeQuery",
				})
			}),
			Shell: shellArm(root, `grep -rn 'cleanNudgeQuery' --include='*.go' .`),
		},
		{
			Name:     "orient in the repo",
			Question: "What does this repository's structure look like?",
			Jade: jadeArm(func() (interface{}, error) {
				return api.WorkspaceTree(protocol.WorkspaceTreeRequest{MaxEntries: 60})
			}),
			Shell: shellArm(root, `find . -path ./.git -prune -o -type f -print | head -60`),
		},
		{
			Name:     "what changed",
			Question: "What has changed in the working tree and by how much?",
			Jade: jadeArm(func() (interface{}, error) {
				return api.Changes(), nil
			}),
			Shell: shellArm(root, `git diff --numstat HEAD && git ls-files --others --exclude-standard`),
		},
		{
			Name:     "read a line range",
			Question: "What are lines 30-60 of internal/jobs/runner.go?",
			Jade: jadeArm(func() (interface{}, error) {
				return api.ReadRange(protocol.ReadRangeRequest{
					Path:      "internal/jobs/runner.go",
					StartLine: 30,
					EndLine:   60,
				})
			}),
			Shell: shellArm(root, `sed -n '30,60p' internal/jobs/runner.go`),
		},
	}
}

// jadeArm measures a jade response exactly as the agent receives it over
// MCP: rendered text where a renderer exists, JSON otherwise. Measuring the
// Go struct, or a hand-trimmed rendering the transport does not actually
// send, would flatter jade for a cost the agent still pays.
func jadeArm(call func() (interface{}, error)) Arm {
	return func() (string, int, error) {
		response, err := call()
		if err != nil {
			return "", 1, err
		}
		if text, ok := render.Text(response); ok {
			return text, 1, nil
		}
		encoded, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return "", 1, err
		}
		return string(encoded), 1, nil
	}
}

// shellArm runs a real shell command and returns its real output. Call count
// is the number of distinct commands an agent would have to issue, which is
// why compound commands are counted by their `&&` segments rather than as
// one.
func shellArm(root string, command string) Arm {
	return func() (string, int, error) {
		cmd := exec.Command("sh", "-c", command)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		calls := strings.Count(command, "&&") + 1
		if err != nil {
			// grep exits 1 on no matches, which is an answer, not a failure.
			// Only treat it as an error when nothing came back at all.
			if len(output) == 0 {
				return "", calls, fmt.Errorf("%s: %w", command, err)
			}
		}
		return string(output), calls, nil
	}
}
