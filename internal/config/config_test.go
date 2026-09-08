package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMonitorMode(t *testing.T) {
	tests := []struct {
		name string
		set  string
		want string
	}{
		{"explicit all", MonitorsAll, MonitorsAll},
		{"explicit active", MonitorsActive, MonitorsActive},
		{"explicit primary", MonitorsPrimary, MonitorsPrimary},
		{"unset falls back", "", MonitorsPrimary},
		{"typo falls back rather than disabling the alert", "primry", MonitorsPrimary},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{OverlayMonitors: tc.set}
			if got := cfg.MonitorMode(); got != tc.want {
				t.Errorf("MonitorMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDurationJSON(t *testing.T) {
	t.Run("round trips as a readable string", func(t *testing.T) {
		data, err := json.Marshal(Duration(90 * time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != `"1m30s"` {
			t.Errorf("marshalled to %s, want \"1m30s\"", data)
		}

		var back Duration
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatal(err)
		}
		if back.Std() != 90*time.Second {
			t.Errorf("round trip gave %v, want 1m30s", back.Std())
		}
	})

	t.Run("accepts bare seconds", func(t *testing.T) {
		var d Duration
		if err := json.Unmarshal([]byte("45"), &d); err != nil {
			t.Fatal(err)
		}
		if d.Std() != 45*time.Second {
			t.Errorf("got %v, want 45s", d.Std())
		}
	})

	t.Run("rejects nonsense", func(t *testing.T) {
		var d Duration
		if err := json.Unmarshal([]byte(`"soon"`), &d); err == nil {
			t.Error("expected an error for an unparseable duration")
		}
	})
}

func TestCalendarSelection(t *testing.T) {
	t.Run("explicit empty selection watches nothing", func(t *testing.T) {
		var cfg Config
		if err := json.Unmarshal([]byte(`{"calendar_ids":[],"calendar_selection_explicit":true}`), &cfg); err != nil {
			t.Fatal(err)
		}

		for _, calendarID := range []string{"ics:work", "ics:personal"} {
			if cfg.Watches(calendarID) {
				t.Errorf("explicit empty selection watched %q", calendarID)
			}
		}
	})

	t.Run("legacy empty selection watches everything", func(t *testing.T) {
		var cfg Config
		if err := json.Unmarshal([]byte(`{"calendar_ids":[]}`), &cfg); err != nil {
			t.Fatal(err)
		}

		for _, calendarID := range []string{"ics:work", "ics:personal"} {
			if !cfg.Watches(calendarID) {
				t.Errorf("legacy empty selection did not watch %q", calendarID)
			}
		}
	})

	t.Run("nonempty selection watches exactly selected calendars", func(t *testing.T) {
		var cfg Config
		if err := json.Unmarshal([]byte(`{"calendar_ids":["ics:work"]}`), &cfg); err != nil {
			t.Fatal(err)
		}

		if !cfg.Watches("ics:work") {
			t.Error("selected calendar was not watched")
		}
		if cfg.Watches("ics:personal") {
			t.Error("unselected calendar was watched")
		}
	})
}

func TestExplicitEmptyCalendarSelectionSurvivesReload(t *testing.T) {
	path := isolateConfigDir(t)
	if err := os.WriteFile(path, []byte(`{"calendar_ids":[],"calendar_selection_explicit":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Watches("ics:work") {
		t.Error("loaded explicit empty selection watched a calendar")
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Watches("ics:work") {
		t.Error("reloaded explicit empty selection watched a calendar")
	}
}

func TestTimeLayout(t *testing.T) {
	if got := (Config{Use24Hour: true}).TimeLayout(); got != "15:04" {
		t.Errorf("24h layout = %q", got)
	}
	if got := (Config{Use24Hour: false}).TimeLayout(); got != "3:04 PM" {
		t.Errorf("12h layout = %q", got)
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

	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	return path
}

func TestLoadDerivesMissingSourceIDs(t *testing.T) {
	path := isolateConfigDir(t)

	body := `{"ics_sources": [{"url": "https://example.com/cal.ics"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.ICSSources) != 1 {
		t.Fatalf("got %d sources, want 1", len(cfg.ICSSources))
	}
	if got, want := cfg.ICSSources[0].ID, SourceID("https://example.com/cal.ics"); got != want {
		t.Errorf("derived ID = %q, want %q", got, want)
	}
}

func TestLoadNormalizesSourceURLs(t *testing.T) {
	path := isolateConfigDir(t)

	// A hand-edited config. The webcal source must be canonicalised, and
	// its ID must hash the canonical form so the same feed spelled either
	// way keeps one identity. The unusable one is kept verbatim: one bad
	// line must not discard every other setting, and the provider lists
	// an unfetchable source so it can be removed in preferences.
	body := `{"ics_sources": [
		{"url": "webcal://example.com/private-abc/basic.ics"},
		{"id": "keepme", "url": "webcal://example.com/other.ics"},
		{"url": "ftp://example.com/nope"},
		{"url": "https://calendar.example/%zz-private-feed.ics"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load must not fail on an unusable source: %v", err)
	}
	if len(cfg.ICSSources) != 4 {
		t.Fatalf("got %d sources, want all 4 kept", len(cfg.ICSSources))
	}

	if got, want := cfg.ICSSources[0].URL, "https://example.com/private-abc/basic.ics"; got != want {
		t.Errorf("URL[0] = %q, want %q", got, want)
	}
	if got, want := cfg.ICSSources[0].ID, SourceID("https://example.com/private-abc/basic.ics"); got != want {
		t.Errorf("ID[0] = %q, want %q derived from the canonical URL", got, want)
	}

	// An explicitly stored ID is what a watch selection references, so
	// rewriting the URL must not change it.
	if cfg.ICSSources[1].ID != "keepme" {
		t.Errorf("ID[1] = %q, want the saved ID preserved", cfg.ICSSources[1].ID)
	}
	if got, want := cfg.ICSSources[1].URL, "https://example.com/other.ics"; got != want {
		t.Errorf("URL[1] = %q, want %q", got, want)
	}

	if got, want := cfg.ICSSources[2].URL, "ftp://example.com/nope"; got != want {
		t.Errorf("URL[2] = %q, want the unusable URL kept verbatim", got)
	}
	if got, want := cfg.ICSSources[3].URL, "https://calendar.example/%zz-private-feed.ics"; got != want {
		t.Errorf("URL[3] = %q, want malformed remote URL kept verbatim", got)
	}
}

func TestNormalizeCalendarURL(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("webcal becomes https", func(t *testing.T) {
		got, err := NormalizeCalendarURL("webcal://host/c.ics")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://host/c.ics" {
			t.Errorf("got %q, want https://host/c.ics", got)
		}
	})

	t.Run("relative path becomes absolute", func(t *testing.T) {
		got, err := NormalizeCalendarURL("./cal.ics")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := filepath.Join(cwd, "cal.ics"); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("windows drive letter is a path, not a scheme", func(t *testing.T) {
		// "C:\cal.ics" parses as a URL with Scheme == "c". Rejecting it
		// would make the feature unusable on Windows.
		got, err := NormalizeCalendarURL(`C:\cal.ics`)
		if err != nil {
			t.Fatalf("rejected a Windows path: %v", err)
		}
		if !strings.HasSuffix(strings.ToLower(got), ".ics") {
			t.Errorf("got %q, want a path ending in .ics", got)
		}
	})

	t.Run("http URL needs no .ics extension", func(t *testing.T) {
		got, err := NormalizeCalendarURL("https://host/page")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://host/page" {
			t.Errorf("got %q, want it unchanged", got)
		}
	})

	t.Run("unsupported scheme errors do not echo the URL", func(t *testing.T) {
		_, err := NormalizeCalendarURL("ftp://host/private-calendar-token")
		if err == nil {
			t.Fatal("expected an error for ftp://")
		}
		if strings.Contains(err.Error(), "private-calendar-token") {
			t.Errorf("error leaked the subscription URL: %v", err)
		}
	})

	t.Run("malformed remote URL is rejected without leaking it", func(t *testing.T) {
		const feed = "https://calendar.example/%zz-private-feed.ics"
		_, err := NormalizeCalendarURL(feed)
		if err == nil {
			t.Fatal("expected malformed remote URL to be rejected")
		}
		if strings.Contains(err.Error(), "zz-private-feed") {
			t.Errorf("error leaked the subscription URL: %v", err)
		}
	})

	t.Run("path without .ics is rejected", func(t *testing.T) {
		if _, err := NormalizeCalendarURL("./calendar"); err == nil {
			t.Error("expected an error for a path with no .ics extension")
		}
	})

	t.Run("empty is rejected", func(t *testing.T) {
		if _, err := NormalizeCalendarURL("   "); err == nil {
			t.Error("expected an error for an empty URL")
		}
	})
}

func TestWatchPreservesSelectionSemantics(t *testing.T) {
	t.Run("legacy empty selection stays all", func(t *testing.T) {
		cfg := Config{}.Watch("ics:abc12345")
		for _, calendarID := range []string{"ics:abc12345", "ics:work"} {
			if !cfg.Watches(calendarID) {
				t.Errorf("legacy empty selection did not watch %q", calendarID)
			}
		}
	})

	t.Run("narrowed selection gains the new calendar", func(t *testing.T) {
		cfg := Config{CalendarIDs: []string{"ics:work"}}.Watch("ics:abc12345")
		if !cfg.Watches("ics:work") || !cfg.Watches("ics:abc12345") {
			t.Error("selected calendars were not watched")
		}
		if cfg.Watches("ics:personal") {
			t.Error("unselected calendar was watched")
		}
	})

	t.Run("explicit empty selection enables only the new calendar", func(t *testing.T) {
		var original Config
		if err := json.Unmarshal([]byte(`{"calendar_ids":[],"calendar_selection_explicit":true}`), &original); err != nil {
			t.Fatal(err)
		}

		cfg := original.Watch("ics:abc12345")
		if original.Watches("ics:abc12345") {
			t.Error("Watch changed the original explicit empty selection")
		}
		if !cfg.Watches("ics:abc12345") {
			t.Error("new calendar was not watched")
		}
		if cfg.Watches("ics:work") {
			t.Error("unselected calendar was watched")
		}
	})

	t.Run("results have independent selections", func(t *testing.T) {
		ids := make([]string, 1, 4)
		ids[0] = "ics:work"
		base := Config{CalendarIDs: ids}

		a := base.Watch("ics:aaaaaaaa")
		b := base.Watch("ics:bbbbbbbb")

		if !a.Watches("ics:work") || !a.Watches("ics:aaaaaaaa") || a.Watches("ics:bbbbbbbb") {
			t.Errorf("first result selected the wrong calendars")
		}
		if !b.Watches("ics:work") || !b.Watches("ics:bbbbbbbb") || b.Watches("ics:aaaaaaaa") {
			t.Errorf("second result selected the wrong calendars")
		}
	})
}
