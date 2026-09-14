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
	release, name := fakeRelease(t, false)
	dir := filepath.Join(t.TempDir(), "bin")

	out, err := runInstall(t, release, dir)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "installed jade-mcp "+version) {
		t.Fatalf("expected the installed version reported, got:\n%s", out)
	}
	info, err := os.Stat(filepath.Join(dir, "jade-mcp"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("jade-mcp not installed as an executable (%s): %v", name, err)
	}
}

func TestInstallScriptRefusesAChecksumMismatch(t *testing.T) {
	release, _ := fakeRelease(t, true)
	dir := filepath.Join(t.TempDir(), "bin")

	out, err := runInstall(t, release, dir)
	if err == nil || !strings.Contains(out, "checksum mismatch") {
		t.Fatalf("expected a checksum refusal, got err %v:\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "jade-mcp")); err == nil {
		t.Fatal("a binary was installed despite the mismatch")
	}
}

func runInstall(t *testing.T, release string, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", filepath.Join("..", "..", "install.sh"))
	cmd.Env = append(os.Environ(),
		"JADE_VERSION="+version,
		"JADE_RELEASE_URL=file://"+release,
		"JADE_INSTALL_DIR="+dir,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// fakeRelease writes <release>/<version>/jade-mcp_<version>_<os>_<arch>.tar.gz
// holding a stand-in binary, and checksums.txt in sha256sum's format. corrupt
// lists a wrong checksum.
func fakeRelease(t *testing.T, corrupt bool) (string, string) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("install.sh supports darwin and linux")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
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
	binary := []byte("#!/bin/sh\necho jade-mcp " + version + "\n")
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
	return release, name
}
