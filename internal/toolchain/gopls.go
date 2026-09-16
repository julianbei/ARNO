// Package toolchain locates external developer tools ARNO shells out to.
package toolchain

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	goplsOnce sync.Once
	goplsBin  string
	goplsOK   bool
)

// Gopls returns the path to the gopls binary and whether it was found.
//
// PATH alone is not enough: gopls is installed with
// `go install golang.org/x/tools/gopls@latest`, which writes to GOBIN or
// $GOPATH/bin, and neither is on PATH by default on a stock macOS or Linux
// setup. Treating "not on PATH" as "not installed" makes ARNO report gopls
// as unavailable on machines where the user has installed it exactly as the
// Go documentation instructs — which is precisely what happened on this
// repo's own development machine.
//
// The result is cached: the answer cannot change within a process, and the
// lookup would otherwise run on every diagnostic, reference and rename.
func Gopls() (string, bool) {
	goplsOnce.Do(func() {
		goplsBin, goplsOK = findGopls()
	})
	return goplsBin, goplsOK
}

func findGopls() (string, bool) {
	if path, err := exec.LookPath("gopls"); err == nil {
		return path, true
	}

	for _, dir := range goBinDirs() {
		candidate := filepath.Join(dir, goplsBinaryName())
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// goBinDirs lists where `go install` may have put a binary, most specific
// first: an explicit GOBIN wins, then each GOPATH entry's bin directory,
// then the default $HOME/go/bin for the common case where neither variable
// is set at all.
func goBinDirs() []string {
	dirs := make([]string, 0, 4)

	if gobin := os.Getenv("GOBIN"); gobin != "" {
		dirs = append(dirs, gobin)
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		for _, entry := range filepath.SplitList(gopath) {
			if entry != "" {
				dirs = append(dirs, filepath.Join(entry, "bin"))
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	return dirs
}

func goplsBinaryName() string {
	if runtime.GOOS == "windows" {
		return "gopls.exe"
	}
	return "gopls"
}
