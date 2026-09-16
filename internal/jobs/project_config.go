package jobs

import (
	"path/filepath"
	"strings"

	"github.com/julianbei/arno/internal/project"
)

// configuredTestCommand is the test command .arno/project.json declares for a
// scope, if it declares one. An invalid config is an error rather than a
// fallback to discovery: it would run commands nobody declared.
func configuredTestCommand(dir string, scope TestScope) (string, bool, error) {
	config, err := project.Load(dir)
	if err != nil || config == nil {
		return "", false, err
	}
	switch scope.Kind {
	case "", "all":
		command, ok := config.Command("tests")
		return command, ok, nil
	case "file":
		command, ok := config.TestFileCommand(scope.File)
		return command, ok, nil
	case "test":
		command, ok := config.TestNameCommand(scope.File, scope.Test)
		return command, ok, nil
	}
	return "", false, nil
}

// DraftProjectArea describes the workspace at dir as one area, from what
// discovery finds, for `arno-mcp init` to write as a starting config.
func DraftProjectArea(dir string) project.Area {
	area := project.Area{Path: ".", Language: detectLanguage(dir)}
	for kind, field := range map[string]*string{"build": &area.Build, "typecheck": &area.Typecheck, "tests": &area.Test} {
		if name, args, ok := discoverCommand(dir, kind); ok {
			*field = shellJoin(name, args)
		}
	}
	if area.Language == "go" {
		area.TestName = "go test -run {name} ./..."
		return area
	}
	// A Rust file's package depends on where the file is, which a fixed
	// command cannot say; discovery keeps choosing -p per file.
	if area.Language != "rust" {
		if name, args, err := scopedTestCommand(dir, TestScope{Kind: "file", File: "{file}"}, nil); err == nil && name != "" {
			area.TestFile = shellJoin(name, args)
		}
	}
	if name, args, err := scopedTestCommand(dir, TestScope{Kind: "test", Test: "{name}"}, nil); err == nil && name != "" {
		area.TestName = shellJoin(name, args)
	}
	return area
}

func detectLanguage(dir string) string {
	has := func(name string) bool { return fileExists(filepath.Join(dir, name)) }
	switch {
	case has("go.mod"):
		return "go"
	case has("Cargo.toml"):
		return "rust"
	case has("package.json") && has("tsconfig.json"):
		return "typescript"
	case has("package.json"):
		return "javascript"
	case isPythonProject(dir):
		return "python"
	case has("pom.xml"), has("build.gradle"), has("build.gradle.kts"):
		return "java"
	case has("build.sbt"):
		return "scala"
	case has("Gemfile"):
		return "ruby"
	}
	return ""
}

// shellJoin writes a command line, quoting arguments that need it. A
// placeholder is left bare for project.fill to quote.
func shellJoin(name string, args []string) string {
	words := []string{name}
	for _, arg := range append([]string(nil), args...) {
		if strings.Contains(arg, "{") || !strings.ContainsAny(arg, " \t'\"$`*?;&|<>()\\") {
			words = append(words, arg)
			continue
		}
		words = append(words, project.Quote(arg))
	}
	return strings.Join(words, " ")
}
