package ics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spriz/meeting-blaster/internal/config"
)

// TestLoadedWebcalSourceIsFetchable is the seam between config.Load and this
// provider. config.json is documented as hand-editable, so a raw
// "webcal://" URL can reach the provider - where localPath treats any
// non-http scheme as a filesystem path, so os.Stat fails and the feed
// silently never loads. config.Load must hand over a canonical https URL.
//
// It lives here rather than in internal/config because config cannot import
// the provider: ics imports config.
func TestLoadedWebcalSourceIsFetchable(t *testing.T) {
	body, err := os.ReadFile("testdata/links.ics")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// TLS, because normalising webcal:// yields https://.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	webcalURL := strings.Replace(srv.URL, "https://", "webcal://", 1) + "/cal.ics"

	// A hand-edited config: webcal scheme, and no id to derive from.
	path := isolateConfigDir(t)
	saved := map[string]any{
		"ics_sources": []map[string]string{{"url": webcalURL}},
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.ICSSources) != 1 {
		t.Fatalf("got %d sources, want 1", len(cfg.ICSSources))
	}

	src := cfg.ICSSources[0]
	if !strings.HasPrefix(src.URL, "https://") {
		t.Fatalf("Load left the URL as %q; the provider will treat it as a file path", src.URL)
	}
	// The ID must hash the canonical URL, so the same feed spelled either
	// way keeps one identity and one watch selection.
	if want := config.SourceID(src.URL); src.ID != want {
		t.Errorf("ID = %q, want %q derived from the canonical URL", src.ID, want)
	}

	p := New([]Source{{ID: src.ID, Name: src.Name, URL: src.URL, Email: src.Email}}, quiet())
	p.client = srv.Client() // trust the test server's certificate

	from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)

	events, err := p.Events(context.Background(), from, to)
	if err != nil {
		t.Fatalf("Events on a config-loaded webcal source: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %s", len(events), describe(events))
	}
	if events[0].CalendarID != src.ID {
		t.Errorf("CalendarID = %q, want the source ID %q", events[0].CalendarID, src.ID)
	}
}

// isolateConfigDir points os.UserConfigDir at a temp directory on every
// platform CI builds for, and returns the config.json path inside it.
func isolateConfigDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // linux
	t.Setenv("HOME", dir)            // darwin: $HOME/Library/Application Support
	t.Setenv("AppData", dir)         // windows

	path, err := config.Path()
	if err != nil {
		t.Fatalf("config.Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	return path
}
