package update

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func checker(t *testing.T, current string, handler http.HandlerFunc) (*Checker, *atomic.Int32, *time.Time) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	c := &Checker{
		Current:   current,
		CachePath: filepath.Join(t.TempDir(), "update-check.json"),
		URL:       server.URL,
		Now:       func() time.Time { return now },
		Client:    server.Client(),
		Getenv:    func(string) string { return "" },
	}
	return c, &hits, &now
}

func latest(tag string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}
}

func TestANewerReleaseIsNoticedAfterARefresh(t *testing.T) {
	c, _, _ := checker(t, "v0.0.8", latest("v0.0.9"))
	if notice := c.Notice(); notice != "" {
		t.Fatalf("no notice may come before any check, got %q", notice)
	}
	c.Refresh()
	notice := c.Notice()
	if !strings.Contains(notice, "update available: v0.0.9 (running v0.0.8)") || !strings.Contains(notice, InstallCommand) {
		t.Fatalf("notice: %q", notice)
	}
}

func TestTheSameOrAnOlderReleaseGivesNoNotice(t *testing.T) {
	for _, tag := range []string{"v0.0.8", "v0.0.7"} {
		c, _, _ := checker(t, "v0.0.8", latest(tag))
		c.Refresh()
		if notice := c.Notice(); notice != "" {
			t.Errorf("latest %s: unexpected notice %q", tag, notice)
		}
	}
}

func TestTheCheckRunsAtMostOnceADayEvenWhenItFails(t *testing.T) {
	c, hits, now := checker(t, "v0.0.8", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c.Refresh()
	c.Refresh()
	if hits.Load() != 1 {
		t.Fatalf("a failed check must still wait a day, got %d requests", hits.Load())
	}
	*now = now.Add(25 * time.Hour)
	c.Refresh()
	if hits.Load() != 2 {
		t.Fatalf("a day later the check should run again, got %d requests", hits.Load())
	}
}

func TestTheCheckIsOffWhenDisabledInCIOrForADevBuild(t *testing.T) {
	cases := map[string]func(c *Checker){
		"ARNO_UPDATE_CHECK=0": func(c *Checker) {
			c.Getenv = func(key string) string {
				if key == DisableEnv {
					return "0"
				}
				return ""
			}
		},
		"CI": func(c *Checker) {
			c.Getenv = func(key string) string {
				if key == "CI" {
					return "true"
				}
				return ""
			}
		},
		"dev build": func(c *Checker) { c.Current = "dev" },
	}
	for name, configure := range cases {
		c, hits, _ := checker(t, "v0.0.8", latest("v0.0.9"))
		configure(c)
		c.Refresh()
		if hits.Load() != 0 || c.Notice() != "" {
			t.Errorf("%s: expected no request and no notice, got %d requests", name, hits.Load())
		}
	}
}

func TestVersionsCompareNumerically(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.0.10", "v0.0.9", true},
		{"v0.1.0", "v0.0.99", true},
		{"v0.0.9", "v0.0.10", false},
		{"v0.0.8", "v0.0.8-3-gabc", false},
		{"dev", "v0.0.8", false},
	}
	for _, tc := range cases {
		if got := newer(tc.a, tc.b); got != tc.want {
			t.Errorf("newer(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
