// Package update tells a user that a newer ARNO release exists without getting
// in their way.
//
// The rules that keep it quiet: nothing is fetched on the request path — a
// notice is read from a cache, and the cache is refreshed at most once a day in
// the background with a short timeout; a failed or offline check also waits a
// day; and the notice appears only where someone asked what they are running,
// `arno-mcp --version` and the capabilities report, never in the server
// instructions an agent reads every session. ARNO_UPDATE_CHECK=0, a CI
// environment or a development build turn it off.
//
// The check is one unauthenticated GET of the latest release tag from GitHub.
// It sends nothing about the workspace or its use; telemetry stays local.
package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DisableEnv set to 0 turns the check off.
	DisableEnv = "ARNO_UPDATE_CHECK"
	// InstallCommand installs or updates ARNO in place.
	InstallCommand = "curl -fsSL https://raw.githubusercontent.com/julianbei/arno/main/install.sh | sh"

	latestURL    = "https://api.github.com/repos/julianbei/arno/releases/latest"
	checkEvery   = 24 * time.Hour
	fetchTimeout = 3 * time.Second
)

// Checker compares the running version with the latest release.
type Checker struct {
	Current   string
	CachePath string
	URL       string
	Now       func() time.Time
	Client    *http.Client
	Getenv    func(string) string
}

// New builds a Checker for the running version, caching in the user's cache
// directory.
func New(current string) *Checker {
	path := ""
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		path = filepath.Join(dir, "arno", "update-check.json")
	}
	return &Checker{
		Current:   current,
		CachePath: path,
		URL:       latestURL,
		Now:       time.Now,
		Client:    &http.Client{Timeout: fetchTimeout},
		Getenv:    os.Getenv,
	}
}

type record struct {
	CheckedAt time.Time `json:"checkedAt"`
	Latest    string    `json:"latest,omitempty"`
}

// Enabled reports whether this binary checks at all.
func (c *Checker) Enabled() bool {
	if c.CachePath == "" || strings.TrimSpace(c.Getenv(DisableEnv)) == "0" || c.Getenv("CI") != "" {
		return false
	}
	_, ok := parse(c.Current)
	return ok
}

// Notice is a one-line notice when the cached latest release is newer than the
// running version, and empty otherwise. It never touches the network.
func (c *Checker) Notice() string {
	if !c.Enabled() {
		return ""
	}
	cached, ok := c.read()
	if !ok || !newer(cached.Latest, c.Current) {
		return ""
	}
	return fmt.Sprintf("update available: %s (running %s) · %s, then reconnect your MCP client", cached.Latest, c.Current, InstallCommand)
}

// Refresh fetches the latest release tag when the cache is a day old or
// missing. It is bounded by its client's timeout; call it in the background.
func (c *Checker) Refresh() {
	if !c.Enabled() {
		return
	}
	cached, ok := c.read()
	if ok && c.Now().Sub(cached.CheckedAt) < checkEvery {
		return
	}
	next := record{CheckedAt: c.Now(), Latest: cached.Latest}
	if latest, err := c.fetch(); err == nil {
		next.Latest = latest
	}
	c.write(next)
}

func (c *Checker) fetch() (string, error) {
	request, err := http.NewRequest(http.MethodGet, c.URL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "arno-mcp/"+c.Current)
	response, err := c.Client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("latest release: HTTP %d", response.StatusCode)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return "", err
	}
	if _, ok := parse(release.TagName); !ok {
		return "", fmt.Errorf("latest release tag %q is not a version", release.TagName)
	}
	return release.TagName, nil
}

func (c *Checker) read() (record, bool) {
	data, err := os.ReadFile(c.CachePath)
	if err != nil {
		return record{}, false
	}
	var cached record
	if json.Unmarshal(data, &cached) != nil {
		return record{}, false
	}
	return cached, true
}

// write replaces the cache through a rename, so a process that exits mid-write
// leaves the old cache rather than half a file.
func (c *Checker) write(next record) {
	data, err := json.Marshal(next)
	if err != nil || os.MkdirAll(filepath.Dir(c.CachePath), 0o755) != nil {
		return
	}
	temp := c.CachePath + ".tmp"
	if os.WriteFile(temp, data, 0o644) != nil {
		return
	}
	_ = os.Rename(temp, c.CachePath)
}

// parse reads vMAJOR.MINOR.PATCH, ignoring a pre-release or build suffix.
func parse(version string) ([3]int, bool) {
	var parts [3]int
	core := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if cut := strings.IndexAny(core, "-+"); cut >= 0 {
		core = core[:cut]
	}
	fields := strings.Split(core, ".")
	if len(fields) != 3 {
		return parts, false
	}
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}

// newer reports whether version a is a later release than b.
func newer(a string, b string) bool {
	left, okLeft := parse(a)
	right, okRight := parse(b)
	if !okLeft || !okRight {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return left[i] > right[i]
		}
	}
	return false
}
