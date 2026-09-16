// Package deps finds the source of a project's dependencies: the crate in the
// Cargo registry, the module in the Go module cache, the package in
// node_modules or in the repository's virtual environment.
//
// It exists for read-only lookups. An agent fixing a bug in ripgrep searched
// for a type in regex-syntax and could not reach it, because ARNO reads only
// the workspace. Resolution goes through the project's own lock or manifest,
// so the version read is the one the project builds with, and never searches
// the machine at large.
package deps

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Source is one resolved dependency.
type Source struct {
	Name      string
	Version   string
	Dir       string
	Ecosystem string
}

// Resolve finds the source directory of the dependency called name, as the
// project rooted at root depends on it.
func Resolve(root string, name string) (Source, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "..") || filepath.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return Source{}, fmt.Errorf("invalid dependency name %q", name)
	}

	looked := []string{}
	for _, resolve := range []func(string, string) (Source, string){cargoSource, goSource, nodeSource, pythonSource} {
		source, where := resolve(root, name)
		if source.Dir != "" {
			return source, nil
		}
		if where != "" {
			looked = append(looked, where)
		}
	}
	if len(looked) == 0 {
		return Source{}, fmt.Errorf("dependency %q not found: no Cargo.lock, go.mod, node_modules or virtual environment at the workspace root", name)
	}
	return Source{}, fmt.Errorf("dependency %q not found in %s", name, strings.Join(looked, ", "))
}

func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// cargoSource reads the locked version from Cargo.lock and finds that crate
// in the registry's unpacked sources.
func cargoSource(root string, name string) (Source, string) {
	file, err := os.Open(filepath.Join(root, "Cargo.lock"))
	if err != nil {
		return Source{}, ""
	}
	defer file.Close()

	versions := []string{}
	current := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "[[package]]":
			current = ""
		case strings.HasPrefix(line, "name = "):
			current = strings.Trim(strings.TrimPrefix(line, "name = "), `"`)
		case strings.HasPrefix(line, "version = ") && current == name:
			versions = append(versions, strings.Trim(strings.TrimPrefix(line, "version = "), `"`))
		}
	}
	if scanner.Err() != nil {
		return Source{}, ""
	}

	cargoHome := os.Getenv("CARGO_HOME")
	if cargoHome == "" {
		cargoHome = filepath.Join(homeDir(), ".cargo")
	}
	where := "Cargo.lock and " + filepath.Join(cargoHome, "registry", "src")
	// A crate locked at two versions is read at the newest.
	sort.Sort(sort.Reverse(sort.StringSlice(versions)))
	for _, version := range versions {
		matches, _ := filepath.Glob(filepath.Join(cargoHome, "registry", "src", "*", name+"-"+version))
		for _, dir := range matches {
			if dirExists(dir) {
				return Source{Name: name, Version: version, Dir: dir, Ecosystem: "cargo"}, where
			}
		}
	}
	return Source{}, where
}

// goSource reads the required version from go.mod and finds that module in
// the module cache. name may be the module path or its last element.
func goSource(root string, name string) (Source, string) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return Source{}, ""
	}
	modCache := os.Getenv("GOMODCACHE")
	if modCache == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			gopath = filepath.Join(homeDir(), "go")
		}
		modCache = filepath.Join(filepath.SplitList(gopath)[0], "pkg", "mod")
	}
	where := "go.mod and " + modCache

	inBlock := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if comment := strings.Index(line, "//"); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		switch {
		case line == "require (":
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		case !inBlock:
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		module, version := fields[0], fields[1]
		if module != name && path.Base(module) != name {
			continue
		}
		dir := filepath.Join(modCache, filepath.FromSlash(escapeModulePath(module))+"@"+version)
		if dirExists(dir) {
			return Source{Name: name, Version: version, Dir: dir, Ecosystem: "go"}, where
		}
	}
	return Source{}, where
}

// escapeModulePath applies the module cache's case encoding: an upper-case
// letter is stored as '!' followed by its lower-case form.
func escapeModulePath(module string) string {
	var b strings.Builder
	for _, r := range module {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func nodeSource(root string, name string) (Source, string) {
	modules := filepath.Join(root, "node_modules")
	if !dirExists(modules) {
		return Source{}, ""
	}
	dir := filepath.Join(modules, filepath.FromSlash(name))
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return Source{}, "node_modules"
	}
	var manifest struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(data, &manifest)
	return Source{Name: name, Version: manifest.Version, Dir: dir, Ecosystem: "npm"}, "node_modules"
}

// pythonSource finds a package directory in the repository's virtual
// environment. Distribution names use dashes where import names use
// underscores, and are matched either way.
func pythonSource(root string, name string) (Source, string) {
	for _, venv := range []string{".venv", "venv"} {
		sites, _ := filepath.Glob(filepath.Join(root, venv, "lib", "python*", "site-packages"))
		for _, site := range sites {
			where := filepath.Join(venv, "lib", filepath.Base(filepath.Dir(site)), "site-packages")
			for _, candidate := range []string{name, strings.ReplaceAll(name, "-", "_"), strings.ToLower(strings.ReplaceAll(name, "-", "_"))} {
				dir := filepath.Join(site, candidate)
				if !dirExists(dir) {
					continue
				}
				version := ""
				if infos, _ := filepath.Glob(filepath.Join(site, candidate+"-*.dist-info")); len(infos) > 0 {
					version = strings.TrimSuffix(strings.TrimPrefix(filepath.Base(infos[0]), candidate+"-"), ".dist-info")
				}
				return Source{Name: name, Version: version, Dir: dir, Ecosystem: "python"}, where
			}
			return Source{}, where
		}
	}
	return Source{}, ""
}
