package jobs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Project is a package with its own manifest below the workspace root.
type Project struct {
	Path     string
	Manifest string
}

// projectManifests are the files that make a directory its own project.
var projectManifests = []string{
	"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "setup.py",
	"pom.xml", "build.gradle", "build.gradle.kts", "build.sbt", "Gemfile", "Makefile",
}

// skippedProjectDirs hold other people's projects or build output, never the
// repository's own.
var skippedProjectDirs = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "dist": true, "build": true,
	"testdata": true, "fixtures": true, "venv": true,
}

// maxProjectDepth bounds the search: a monorepo's projects sit one or two
// levels down (apps/web, services/api), and a deeper manifest is usually a
// fixture or an example.
const maxProjectDepth = 2

// DiscoverProjects lists the projects below root: every directory up to
// maxProjectDepth deep with a manifest of its own. A project's subdirectories
// belong to it and are not searched further. Hidden directories are skipped.
func DiscoverProjects(root string) []Project {
	var projects []Project
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > 0 {
			for _, manifest := range projectManifests {
				if fileExists(filepath.Join(dir, manifest)) {
					rel, err := filepath.Rel(root, dir)
					if err == nil {
						projects = append(projects, Project{Path: filepath.ToSlash(rel), Manifest: manifest})
					}
					return
				}
			}
		}
		if depth >= maxProjectDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") || skippedProjectDirs[name] {
				continue
			}
			walk(filepath.Join(dir, name), depth+1)
		}
	}
	walk(root, 0)
	sort.Slice(projects, func(a, b int) bool { return projects[a].Path < projects[b].Path })
	return projects
}
