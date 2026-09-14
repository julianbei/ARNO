// Package install tests install.sh against a release laid out on disk the way
// the release workflow publishes it, so the test never downloads from GitHub
// or adds to the real download counts.
package install

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const version = "v9.9.9"

func TestInstallScriptVerifiesAndInstalls(t *testing.T) {
	release := fakeRelease(t, false)
	dir := filepath.Join(t.TempDir(), "bin")

	out, err := runInstall(t, release, "JADE_INSTALL_DIR="+dir)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "installed jade-mcp "+version) {
		t.Fatalf("expected the installed version reported, got:\n%s", out)
	}
	info, err := os.Stat(filepath.Join(dir, "jade-mcp"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("jade-mcp not installed as an executable: %v", err)
	}
}

func TestInstallScriptRefusesAChecksumMismatch(t *testing.T) {
	release := fakeRelease(t, true)
	dir := filepath.Join(t.TempDir(), "bin")

	out, err := runInstall(t, release, "JADE_INSTALL_DIR="+dir)
	if err == nil || !strings.Contains(out, "checksum mismatch") {
		t.Fatalf("expected a checksum refusal, got err %v:\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "jade-mcp")); err == nil {
		t.Fatal("a binary was installed despite the mismatch")
	}
}

// An older jade-mcp on PATH is replaced where it is, not installed a second
// time somewhere else.
func TestInstallScriptUpdatesTheBinaryOnPath(t *testing.T) {
	release := fakeRelease(t, false)
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "jade-mcp"), "v0.0.1")

	out, err := runInstall(t, release, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "updated jade-mcp v0.0.1 -> "+version+" in "+dir) {
		t.Fatalf("expected an in-place update, got:\n%s", out)
	}
	got, err := exec.Command(filepath.Join(dir, "jade-mcp"), "--version").Output()
	if err != nil || !strings.Contains(string(got), version) {
		t.Fatalf("the binary on PATH was not replaced: %q %v", got, err)
	}
}

// A binary already at the release is left alone, and nothing is downloaded.
func TestInstallScriptLeavesACurrentBinaryAlone(t *testing.T) {
	dir := t.TempDir()
	writeFakeBinary(t, filepath.Join(dir, "jade-mcp"), version)

	// No release on disk at all: any download attempt would fail the run.
	out, err := runInstall(t, filepath.Join(t.TempDir(), "no-release"), "JADE_INSTALL_DIR="+dir)
	if err != nil || !strings.Contains(out, "jade-mcp "+version+" is already installed") {
		t.Fatalf("expected no-op, got err %v:\n%s", err, out)
	}
}

// An agent has no terminal: JADE_SERVERS picks servers up front, and without
// it the script names the non-interactive commands.
func TestInstallScriptTakesServersWithoutATerminal(t *testing.T) {
	release := fakeRelease(t, false)

	out, err := runInstall(t, release, "JADE_INSTALL_DIR="+filepath.Join(t.TempDir(), "bin"), "JADE_SERVERS=go,java")
	if err != nil || !strings.Contains(out, "install --servers go,java") {
		t.Fatalf("expected the servers passed on, got err %v:\n%s", err, out)
	}

	out, err = runInstall(t, release, "JADE_INSTALL_DIR="+filepath.Join(t.TempDir(), "bin"))
	if err != nil || !strings.Contains(out, "jade-mcp install --list --json") {
		t.Fatalf("expected the agent hint, got err %v:\n%s", err, out)
	}
}

// An install directory off PATH is added to the shell profile once, so
// `jade-mcp` is not "command not found" in the next terminal.
func TestInstallScriptAddsTheDirectoryToPathOnce(t *testing.T) {
	release := fakeRelease(t, false)
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	env := []string{"HOME=" + home, "SHELL=/bin/zsh", "JADE_ADD_TO_PATH=1", "JADE_INSTALL_DIR=" + dir}

	for run := 0; run < 2; run++ {
		if run == 1 {
			// Force a reinstall so the PATH step runs again.
			if err := os.Remove(filepath.Join(dir, "jade-mcp")); err != nil {
				t.Fatal(err)
			}
		}
		out, err := runInstall(t, release, env...)
		if err != nil || !strings.Contains(out, "use this command: "+dir+"/jade-mcp") {
			t.Fatalf("run %d: err %v:\n%s", run, err, out)
		}
	}
	profile, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(profile), "export PATH=\""+dir+":$PATH\""); got != 1 {
		t.Fatalf("expected the PATH line once, found %d:\n%s", got, profile)
	}
}

func runInstall(t *testing.T, release string, env ...string) (string, error) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("install.sh supports darwin and linux")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
	cmd := exec.Command("sh", filepath.Join("..", "..", "install.sh"))
	cmd.Env = append(append(os.Environ(),
		"JADE_VERSION="+version,
		"JADE_RELEASE_URL=file://"+release,
	), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeFakeBinary(t *testing.T, path string, reports string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho jade-mcp "+reports+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakeRelease writes <release>/<version>/jade-mcp_<version>_<os>_<arch>.tar.gz
// holding a stand-in binary, and checksums.txt in sha256sum's format. corrupt
// lists a wrong checksum.
func fakeRelease(t *testing.T, corrupt bool) string {
	t.Helper()
	name := "jade-mcp_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH
	release := t.TempDir()
	dir := filepath.Join(release, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(dir, name+".tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zipped := gzip.NewWriter(file)
	tarball := tar.NewWriter(zipped)
	// Echoes its arguments after the version, so a test sees how install.sh
	// called it; `--version` still reads as the version in field two.
	binary := []byte("#!/bin/sh\necho jade-mcp " + version + " \"$@\"\n")
	if err := tarball.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarball.Write(binary); err != nil {
		t.Fatal(err)
	}
	for _, closer := range []interface{ Close() error }{tarball, zipped, file} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if corrupt {
		digest = strings.Repeat("0", len(digest))
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(digest+"  "+name+".tar.gz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return release
}
