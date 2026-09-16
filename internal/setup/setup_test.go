package setup

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/lsp"
)

// fakeMachine has the tools named in available, and records what it ran.
// installing a component makes its Detect succeed.
type fakeMachine struct {
	available map[string]bool
	installed map[string]bool
	ran       []string
	fail      map[string]bool
}

func (f *fakeMachine) env() Environment {
	return Environment{
		LookPath: func(name string) (string, error) {
			if f.available[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Run: func(command []string, out io.Writer) error {
			line := strings.Join(command, " ")
			f.ran = append(f.ran, line)
			if f.fail[command[0]] {
				return errors.New("exit status 1")
			}
			f.installed[command[len(command)-1]] = true
			return nil
		},
	}
}

func (f *fakeMachine) component(key string, name string, needs string) Component {
	return Component{
		Key: key, Kind: "language server", Name: name,
		Detect: func() (string, bool) {
			if f.installed[name] {
				return "/opt/bin/" + name, true
			}
			return "", false
		},
		Recipes: []Recipe{{Needs: needs, Command: []string{needs, "install", name}}},
		Manual:  "install " + name + " by hand",
	}
}

func menu(f *fakeMachine, input string) (Menu, *bytes.Buffer) {
	var out bytes.Buffer
	return Menu{
		In:  strings.NewReader(input),
		Out: &out,
		Env: f.env(),
		Components: []Component{
			f.component("go", "gopls", "go"),
			f.component("java", "jdtls", "brew"),
			f.component("scala", "metals", "cs"),
		},
	}, &out
}

func TestTheMenuInstallsWhatIsPickedAfterConfirming(t *testing.T) {
	f := &fakeMachine{available: map[string]bool{"go": true, "brew": true}, installed: map[string]bool{}}
	m, out := menu(f, "1 java\n\n")
	if err := m.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if strings.Join(f.ran, "; ") != "go install gopls; brew install jdtls" {
		t.Fatalf("ran %q\n%s", f.ran, out)
	}
	if !strings.Contains(out.String(), "gopls installed at /opt/bin/gopls") {
		t.Fatalf("expected the new location reported:\n%s", out)
	}
}

func TestTheListShowsTheCommandBeforeAnythingRuns(t *testing.T) {
	f := &fakeMachine{available: map[string]bool{"go": true}, installed: map[string]bool{"jdtls": true}}
	m, out := menu(f, "")
	m.List()
	text := out.String()
	for _, want := range []string{"would run: go install gopls", "installed · /opt/bin/jdtls", "by hand: install metals by hand"} {
		if !strings.Contains(text, want) {
			t.Errorf("list is missing %q:\n%s", want, text)
		}
	}
	if len(f.ran) != 0 {
		t.Fatalf("listing ran %q", f.ran)
	}
}

func TestDecliningTheConfirmationInstallsNothing(t *testing.T) {
	f := &fakeMachine{available: map[string]bool{"go": true}, installed: map[string]bool{}}
	m, out := menu(f, "1\nn\n")
	if err := m.Run(); err != nil || len(f.ran) != 0 {
		t.Fatalf("declined, but ran %q (err %v)\n%s", f.ran, err, out)
	}
}

func TestAllSkipsInstalledAndExplainsWhatHasNoInstaller(t *testing.T) {
	f := &fakeMachine{available: map[string]bool{"brew": true}, installed: map[string]bool{"gopls": true}}
	m, out := menu(f, "a\ny\n")
	if err := m.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if strings.Join(f.ran, "; ") != "brew install jdtls" {
		t.Fatalf("ran %q\n%s", f.ran, out)
	}
	if !strings.Contains(out.String(), "metals: no installer on this machine; by hand: install metals by hand") {
		t.Fatalf("expected manual instructions for metals:\n%s", out)
	}
}

func TestAnUnknownChoiceIsAskedAgain(t *testing.T) {
	f := &fakeMachine{available: map[string]bool{"go": true}, installed: map[string]bool{}}
	m, out := menu(f, "9\nkotlin\ngo\ny\n")
	if err := m.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	text := out.String()
	if !strings.Contains(text, "9 is not in the list") || !strings.Contains(text, `"kotlin" is not in the list`) {
		t.Fatalf("expected both refusals:\n%s", text)
	}
	if strings.Join(f.ran, "; ") != "go install gopls" {
		t.Fatalf("ran %q", f.ran)
	}
}

func TestAFailedInstallIsReportedWithTheManualWay(t *testing.T) {
	f := &fakeMachine{available: map[string]bool{"go": true}, installed: map[string]bool{}, fail: map[string]bool{"go": true}}
	m, out := menu(f, "")
	err := m.Install([]string{"go"}, false, false)
	if !errors.Is(err, ErrInstallFailed) || !strings.Contains(out.String(), "gopls: install failed") {
		t.Fatalf("expected a reported failure, got %v:\n%s", err, out)
	}
	if err := m.Install([]string{"kotlin"}, false, false); err == nil || !strings.Contains(err.Error(), "choose from go, java, scala") {
		t.Fatalf("expected an unknown-key error, got %v", err)
	}
}

// An agent can read the state and preview the commands without anything
// running.
func TestAnAgentCanReadStatusAndPreviewWithoutRunning(t *testing.T) {
	f := &fakeMachine{available: map[string]bool{"go": true}, installed: map[string]bool{"jdtls": true}}
	m, out := menu(f, "")

	statuses := m.Statuses()
	if len(statuses) != 3 || statuses[0].Installed || strings.Join(statuses[0].Command, " ") != "go install gopls" {
		t.Fatalf("gopls status: %+v", statuses)
	}
	if !statuses[1].Installed || statuses[1].Path != "/opt/bin/jdtls" || statuses[1].Command != nil {
		t.Fatalf("jdtls status: %+v", statuses[1])
	}
	if statuses[2].Command != nil || statuses[2].Manual == "" {
		t.Fatalf("metals status: %+v", statuses[2])
	}

	if err := m.Install(nil, true, true); err != nil {
		t.Fatal(err)
	}
	if len(f.ran) != 0 || !strings.Contains(out.String(), "would run: go install gopls") {
		t.Fatalf("dry run ran %q:\n%s", f.ran, out)
	}
}

// Every language ARNO has a server for can be installed from the menu.
func TestEveryLanguageServerIsInTheMenu(t *testing.T) {
	covered := map[string]bool{}
	for _, component := range LanguageServers() {
		if len(component.Recipes) == 0 || component.Manual == "" || component.Detect == nil {
			t.Errorf("%s lacks a recipe, manual instructions or detection", component.Key)
		}
		for _, language := range component.Languages {
			covered[language] = true
		}
	}
	for _, language := range lsp.Languages() {
		if !covered[language] {
			t.Errorf("language %s has a server but no menu entry", language)
		}
	}
}
