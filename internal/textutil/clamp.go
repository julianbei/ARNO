// Package textutil holds text-shaping helpers shared by jade's response
// builders.
package textutil

import (
	"fmt"
	"strings"
)

// ClampHeadFraction splits a clamp budget between the start and end of the
// text. Both ends matter for tool output: compilers report the first failure
// first, `go test` prints its FAIL summary last, and a diff's first and last
// hunks are equally likely to be the interesting one — so keeping only one
// end reliably loses the decisive part for some caller.
const ClampHeadFraction = 0.4

// Clamp bounds text to maxBytes, keeping the head and the tail and replacing
// the middle with an explicit marker naming what was dropped. It returns the
// clamped text and the number of bytes omitted (0 when nothing was cut).
//
// Cuts land on line boundaries wherever possible: a raw byte cut can split a
// UTF-8 rune or leave a half-written path, which costs a reader more than the
// few bytes it saves.
//
// Truncation is always visible. A reader who cannot see that bytes are
// missing will read the remainder as complete, which is a worse failure than
// returning less.
func Clamp(text string, maxBytes int, hint string) (string, int) {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text, 0
	}

	headBudget := int(float64(maxBytes) * ClampHeadFraction)
	tailBudget := maxBytes - headBudget

	head := trimToLineBoundary(text[:headBudget], false)
	tail := trimToLineBoundary(text[len(text)-tailBudget:], true)

	omitted := len(text) - len(head) - len(tail)
	if hint == "" {
		hint = "narrow the scope to see the middle"
	}
	marker := fmt.Sprintf("\n\n... %d bytes omitted (clamped to %d bytes; %s) ...\n\n",
		omitted, maxBytes, hint)

	return head + marker + tail, omitted
}

// trimToLineBoundary drops a partial line at the cut edge. When fromStart is
// true the chunk is the tail of the text, so its leading partial line goes;
// otherwise the chunk is the head and its trailing partial line goes. A chunk
// with no newline at all has nothing safe to trim, so it is returned
// unchanged rather than emptied.
func trimToLineBoundary(chunk string, fromStart bool) string {
	if fromStart {
		if index := strings.IndexByte(chunk, '\n'); index >= 0 {
			return chunk[index+1:]
		}
		return chunk
	}
	if index := strings.LastIndexByte(chunk, '\n'); index >= 0 {
		return chunk[:index]
	}
	return chunk
}
