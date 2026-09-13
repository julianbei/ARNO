package main

// coreTools are the tools an agent should load before its first edit.
//
// Hosts load MCP tool schemas lazily: the first call to any tool first costs a
// schema-search turn. A worker with a four-turn budget spent one of them
// discovering `workspace_tree`. Naming the handful that cover most sessions
// lets a host load them in one go. Kept as a list, not just prose, so a test
// can prove every name here is a real tool.
var coreTools = []string{
	"jade.find",
	"jade.read_range",
	"jade.outline",
	"jade.replace_text",
	"jade.insert",
	"jade.apply",
	"jade.check",
}

// serverInstructions is sent once per session, so every sentence is paid for
// by every session. It says what to load, when to batch, and the one naming
// fact that otherwise costs a failed call.
const serverInstructions = "Jade inspects, edits and validates code in this workspace. " +
	"Load these first: jade.find (declaration and body in one call), jade.read_range, jade.outline, " +
	"jade.replace_text, jade.insert, jade.apply, jade.check. " +
	"Edits return diagnostics inline, so a build is rarely needed to see a mistake. " +
	"Changing more than one site? Use jade.apply: one atomic call, one validation at the end, " +
	"and no errors reported from half-finished intermediate states. " +
	"Tool names are accepted as jade.<name> or jade_<name>."
