package main

// coreTools are the tools an agent should load before its first edit.
//
// Hosts load MCP tool schemas lazily: the first call to any tool first costs a
// schema-search turn. A worker with a four-turn budget spent one of them
// discovering `workspace_tree`. Naming the handful that cover most sessions
// lets a host load them in one go. Kept as a list, not just prose, so a test
// can prove every name here is a real tool.
var coreTools = []string{
	"arno.find",
	"arno.read_range",
	"arno.replace_text",
	"arno.insert",
	"arno.apply",
	"arno.check",
}

// serverInstructions is sent once per session, so every sentence is paid for
// by every session. It says what to load, when to batch, and the one naming
// fact that otherwise costs a failed call.
const serverInstructions = "Arno inspects, edits and validates code in this workspace. " +
	"Load these first: arno.find (declaration and body in one call), arno.read_range, " +
	"arno.replace_text, arno.insert, arno.apply, arno.check. " +
	"Repeatable runs (a repro, a benchmark): arno.declare_command once, then arno.run_command. " +
	"Edits return diagnostics inline, so a build is rarely needed to see a mistake. " +
	"Changing more than one site? Use arno.apply: one atomic call, one validation at the end, " +
	"and no errors reported from half-finished intermediate states. " +
	"Tool names are accepted as arno.<name> or arno_<name>."
