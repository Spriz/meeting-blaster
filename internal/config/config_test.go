package config

import (
	"encoding/json"
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

func TestWatches(t *testing.T) {
	t.Run("empty selection watches everything", func(t *testing.T) {
		if !(Config{}).Watches("anything") {
			t.Error("empty CalendarIDs should watch all calendars")
		}
	})
	t.Run("selection is respected", func(t *testing.T) {
		cfg := Config{CalendarIDs: []string{"work"}}
		if !cfg.Watches("work") {
			t.Error("selected calendar should be watched")
		}
		if cfg.Watches("personal") {
			t.Error("unselected calendar should not be watched")
		}
	})
}

func TestTimeLayout(t *testing.T) {
	if got := (Config{Use24Hour: true}).TimeLayout(); got != "15:04" {
		t.Errorf("24h layout = %q", got)
	}
	if got := (Config{Use24Hour: false}).TimeLayout(); got != "3:04 PM" {
		t.Errorf("12h layout = %q", got)
	}
}
