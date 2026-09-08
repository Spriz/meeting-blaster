// Package config handles on-disk user settings.
//
// Settings live in a plain JSON file under the XDG config directory so they
// can be edited by hand. Secrets never go here - see internal/tokens.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AppName is used for config/cache directory names and the keyring service.
const AppName = "meeting-blaster"

// Config is the full set of user-tunable settings.
type Config struct {
	// CalendarIDs limits which calendars are watched. Empty means all.
	CalendarIDs []string `json:"calendar_ids"`

	// ICSSources are iCalendar subscriptions: webcal:// or https:// feed
	// URLs, or paths to .ics files on disk. They need no account and no
	// OAuth client, which is the only way to run without a Google API
	// project.
	ICSSources []ICSSource `json:"ics_sources"`

	// AlertLead is how long before a meeting the full-screen overlay
	// appears. This is the app's headline feature: it is deliberately
	// impossible to miss, so the default is short enough to be actionable
	// but long enough to walk back to your desk.
	AlertLead Duration `json:"alert_lead"`

	// NotifyLead is how long before a meeting a normal desktop
	// notification fires. Zero disables it.
	NotifyLead Duration `json:"notify_lead"`

	// OverlayTimeout auto-dismisses the overlay after this long. Zero
	// leaves it up until dismissed.
	OverlayTimeout Duration `json:"overlay_timeout"`

	// PollInterval is how often the calendar is refetched.
	PollInterval Duration `json:"poll_interval"`

	// Use24Hour selects 15:04 over 3:04 PM.
	Use24Hour bool `json:"use_24_hour"`

	// TitleMaxLen truncates long event titles in the tray label. The top
	// bar is narrow; MeetingBar does the same.
	TitleMaxLen int `json:"title_max_len"`

	// HideDeclined drops events the user responded "no" to.
	HideDeclined bool `json:"hide_declined"`

	// JoinBrowser overrides the browser used for join links. Empty uses
	// the system default handler.
	JoinBrowser string `json:"join_browser"`

	// OverlayMonitors selects which displays the full-screen alert covers:
	// MonitorsPrimary, MonitorsAll, or MonitorsActive.
	OverlayMonitors string `json:"overlay_monitors"`
}

// ICSSource is one iCalendar subscription.
type ICSSource struct {
	// ID is derived from URL by SourceID and stored so that a watch
	// selection survives a rename. Regenerated on load when absent.
	ID string `json:"id"`

	// Name overrides the feed's own X-WR-CALNAME. Optional.
	Name string `json:"name,omitempty"`

	// URL is a canonical https:// URL or an absolute file path.
	URL string `json:"url"`

	// Email marks which ATTENDEE is you, so PARTSTAT=DECLINED can set
	// Event.Declined. An anonymous feed carries no identity, so without
	// this nothing is ever considered declined. Optional.
	Email string `json:"email,omitempty"`
}

// Calendar IDs are namespaced by their source so two providers cannot
// collide: "google:me@example.com", "ics:9f2a1c0b".
const (
	SourceGoogle = "google"
	SourceICS    = "ics"
)

// SourceID derives a stable identifier for a subscription from its
// canonical URL. Deterministic, so re-adding a feed keeps its watch
// selection.
func SourceID(canonicalURL string) string {
	sum := sha256.Sum256([]byte(canonicalURL))
	return hex.EncodeToString(sum[:4])
}

// NormalizeCalendarURL canonicalises a subscription target: webcal:// is
// rewritten to https://, and anything without a usable scheme is treated as
// a filesystem path and made absolute.
func NormalizeCalendarURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("calendar URL is empty")
	}

	unsupported := func() error {
		return fmt.Errorf("unsupported calendar URL %q: expected webcal://, https:// or a path to a .ics file", raw)
	}

	u, err := url.Parse(raw)
	// A single-character scheme is a Windows drive letter: "C:\cal.ics"
	// parses with Scheme == "c". Treating that as a URL scheme would make
	// the feature unusable on Windows.
	if err != nil || u.Scheme == "" || len(u.Scheme) == 1 {
		abs, err := filepath.Abs(raw)
		if err != nil {
			return "", unsupported()
		}
		if !strings.HasSuffix(strings.ToLower(abs), ".ics") {
			return "", unsupported()
		}
		return abs, nil
	}

	switch strings.ToLower(u.Scheme) {
	case "webcal", "webcals":
		u.Scheme = "https"
		return u.String(), nil
	case "http", "https", "file":
		return u.String(), nil
	}
	return "", unsupported()
}

// Values for Config.OverlayMonitors.
const (
	// MonitorsPrimary always blocks the primary display, wherever the
	// pointer happens to be. Predictable: the alert is always in the same
	// place.
	MonitorsPrimary = "primary"

	// MonitorsAll blocks every connected display, with one window each.
	// The hardest to ignore, and the point of the app.
	MonitorsAll = "all"

	// MonitorsActive leaves placement to the window manager, which
	// generally means the display with focus. This is the only mode that
	// works when monitor enumeration is unavailable.
	MonitorsActive = "active"
)

// ValidMonitorModes lists the accepted OverlayMonitors values.
var ValidMonitorModes = []string{MonitorsPrimary, MonitorsAll, MonitorsActive}

// MonitorMode returns OverlayMonitors, falling back to MonitorsPrimary when
// it is unset or unrecognised, so a hand-edited typo cannot stop the alert
// from appearing.
func (c Config) MonitorMode() string {
	for _, mode := range ValidMonitorModes {
		if c.OverlayMonitors == mode {
			return mode
		}
	}
	return MonitorsPrimary
}

// Default returns the settings a fresh install starts with.
func Default() Config {
	return Config{
		AlertLead:       Duration(1 * time.Minute),
		NotifyLead:      Duration(5 * time.Minute),
		OverlayTimeout:  Duration(0),
		PollInterval:    Duration(2 * time.Minute),
		Use24Hour:       true,
		TitleMaxLen:     30,
		HideDeclined:    true,
		OverlayMonitors: MonitorsPrimary,
	}
}

// TimeLayout is the clock format implied by Use24Hour.
func (c Config) TimeLayout() string {
	if c.Use24Hour {
		return "15:04"
	}
	return "3:04 PM"
}

// Watches reports whether the given calendar is selected.
func (c Config) Watches(calendarID string) bool {
	if len(c.CalendarIDs) == 0 {
		return true
	}
	for _, id := range c.CalendarIDs {
		if id == calendarID {
			return true
		}
	}
	return false
}

// Watch returns a copy with calendarID added to the watch list. An empty
// list already means "watch everything", so it is left empty: otherwise
// adding a subscription would narrow the selection to just that one.
// Conversely, a user who has ticked specific calendars would add a
// subscription and silently never see its events.
func (c Config) Watch(calendarID string) Config {
	if len(c.CalendarIDs) == 0 {
		return c
	}
	for _, id := range c.CalendarIDs {
		if id == calendarID {
			return c
		}
	}
	ids := make([]string, len(c.CalendarIDs), len(c.CalendarIDs)+1)
	copy(ids, c.CalendarIDs)
	c.CalendarIDs = append(ids, calendarID)
	return c
}

// migrateCalendarIDs prefixes legacy, unnamespaced calendar IDs with the
// Google source key. Before multi-source support every calendar came from
// Google, so an existing watch selection would otherwise match nothing.
func migrateCalendarIDs(ids []string) []string {
	for i, id := range ids {
		if strings.HasPrefix(id, SourceGoogle+":") || strings.HasPrefix(id, SourceICS+":") {
			continue
		}
		ids[i] = SourceGoogle + ":" + id
	}
	return ids
}

// Dir is the directory holding config.json.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(base, AppName), nil
}

// Path is the full path to config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads settings from disk, returning defaults if none are saved yet.
// Unknown fields are ignored and missing fields keep their default, so a
// config written by an older build still loads.
func Load() (Config, error) {
	cfg := Default()

	path, err := Path()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parse %s: %w", path, err)
	}

	cfg.CalendarIDs = migrateCalendarIDs(cfg.CalendarIDs)
	cfg.ICSSources = normalizeSources(cfg.ICSSources)
	return cfg, nil
}

// normalizeSources canonicalises subscriptions read from disk.
//
// --add-calendar and the preferences window already store a canonical URL,
// but config.json is meant to be hand-editable, and a raw "webcal://" left
// as written would be treated as a filesystem path by the ICS provider and
// silently never fetch. Normalising before deriving the ID also keeps
// SourceID stable across the two spellings of the same feed.
//
// A URL that will not normalise is kept verbatim rather than dropped, and
// never fails the load: one bad hand-edited line must not discard every
// other setting. It surfaces instead as a fetch warning per poll, and the
// provider still lists the source so it can be removed in preferences.
func normalizeSources(sources []ICSSource) []ICSSource {
	for i := range sources {
		if url, err := NormalizeCalendarURL(sources[i].URL); err == nil {
			sources[i].URL = url
		}
		if sources[i].ID == "" {
			sources[i].ID = SourceID(sources[i].URL)
		}
	}
	return sources
}

// Save writes settings to disk, creating the directory if needed.
func Save(cfg Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(dir, "config.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Duration is a time.Duration that round-trips through JSON as a readable
// string such as "5m", rather than a nanosecond integer.
type Duration time.Duration

// Std converts back to the standard library type.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON implements json.Unmarshaler, accepting either a duration
// string ("5m") or a bare number of seconds.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("parse duration %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	}

	var secs float64
	if err := json.Unmarshal(data, &secs); err != nil {
		return fmt.Errorf("duration must be a string like \"5m\" or a number of seconds")
	}
	*d = Duration(time.Duration(secs * float64(time.Second)))
	return nil
}
