package prefs

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
)

func TestSubscriptionLabelKeepsFeedURLsPrivate(t *testing.T) {
	const secretURL = "https://calendar.example/secret-feed-token.ics"
	src := config.ICSSource{ID: "b2f38c1a", URL: secretURL}

	tests := []struct {
		name          string
		source        config.ICSSource
		calendarNames map[string]string
		want          string
	}{
		{
			name:   "configured name wins",
			source: config.ICSSource{ID: src.ID, URL: secretURL, Name: "Personal"},
			calendarNames: map[string]string{
				"ics:" + src.ID: "Feed calendar",
			},
			want: "Personal",
		},
		{
			name: "feed calendar name",
			calendarNames: map[string]string{
				"ics:" + src.ID: "Feed calendar",
			},
			want: "Feed calendar",
		},
		{
			name:          "safe fallback when no name is available",
			calendarNames: map[string]string{},
			want:          "Calendar " + src.ID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.source
			if source.ID == "" {
				source = src
			}
			got := subscriptionLabel(source, tt.calendarNames)
			if got != tt.want {
				t.Errorf("subscriptionLabel() = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, secretURL) {
				t.Errorf("subscriptionLabel() exposed feed URL: %q", got)
			}
		})
	}
}

func TestCalendarControlsSaveAllOff(t *testing.T) {
	isolatePrefsConfigDir(t)

	src := testSource("work")
	cfg := config.Default()
	cfg.ICSSources = []config.ICSSource{src}
	cfg.CalendarIDs = []string{sourceCalendarID(src)}

	var saved config.Config
	w, provider := openTestWindow(t, cfg, func(got config.Config) { saved = got })
	finishCalendarLookup(t, w, provider, calendar.Calendar{ID: sourceCalendarID(src), Name: "Work"})

	show := sourceChecks(w)
	if len(show) != 1 || !show[0].Checked {
		t.Fatal("source should initially be shown")
	}
	test.Tap(show[0])
	if show[0].Checked {
		t.Fatal("Show toggle remained checked")
	}
	test.Tap(button(t, w, "Save"))

	if saved.Watches(sourceCalendarID(src)) {
		t.Fatal("saved all-off selection still watches the source")
	}
	if !reflect.DeepEqual(saved.ICSSources, cfg.ICSSources) {
		t.Fatalf("saved sources = %#v, want %#v", saved.ICSSources, cfg.ICSSources)
	}
}

func TestCalendarControlsRemoveLastShown(t *testing.T) {
	isolatePrefsConfigDir(t)

	shown := testSource("work")
	hidden := testSource("hidden")
	cfg := config.Default()
	cfg.ICSSources = []config.ICSSource{shown, hidden}
	cfg.CalendarIDs = []string{sourceCalendarID(shown)}

	var saved config.Config
	w, provider := openTestWindow(t, cfg, func(got config.Config) { saved = got })
	finishCalendarLookup(t, w, provider,
		calendar.Calendar{ID: sourceCalendarID(shown), Name: "Work"},
		calendar.Calendar{ID: sourceCalendarID(hidden), Name: "Hidden"},
	)

	removes := buttons(t, w, "Remove")
	if len(removes) != 2 {
		t.Fatalf("Remove controls = %d, want 2", len(removes))
	}
	test.Tap(removes[0])
	if checks := sourceChecks(w); len(checks) != 1 || checks[0].Checked {
		t.Fatal("hidden source should remain hidden after removing the last shown source")
	}
	test.Tap(button(t, w, "Save"))

	if !reflect.DeepEqual(saved.ICSSources, []config.ICSSource{hidden}) {
		t.Fatalf("saved sources = %#v, want %#v", saved.ICSSources, []config.ICSSource{hidden})
	}
	if saved.Watches(sourceCalendarID(hidden)) {
		t.Fatal("removing the last shown source revealed a hidden source")
	}
}

func TestCalendarLookupPreservesEditedToggle(t *testing.T) {
	src := testSource("work")
	cfg := config.Default()
	cfg.ICSSources = []config.ICSSource{src}
	cfg.CalendarIDs = []string{sourceCalendarID(src)}

	w, provider := openTestWindow(t, cfg, nil)
	show := sourceChecks(w)
	if len(show) != 1 {
		t.Fatal("expected one Show control before lookup")
	}
	test.Tap(show[0])
	if show[0].Checked {
		t.Fatal("Show toggle remained checked")
	}

	finishCalendarLookup(t, w, provider, calendar.Calendar{ID: sourceCalendarID(src), Name: "Fetched Work"})
	show = sourceChecks(w)
	if len(show) != 1 || show[0].Checked {
		t.Fatal("calendar lookup reset the edited Show toggle")
	}
}

func TestCalendarControlsCancelDoesNotMutateCaller(t *testing.T) {
	first := testSource("first")
	second := testSource("second")
	cfg := config.Default()
	cfg.ICSSources = []config.ICSSource{first, second}
	cfg.CalendarIDs = []string{sourceCalendarID(first), sourceCalendarID(second)}
	wantSources := append([]config.ICSSource(nil), cfg.ICSSources...)

	w, provider := openTestWindow(t, cfg, nil)
	finishCalendarLookup(t, w, provider,
		calendar.Calendar{ID: sourceCalendarID(first), Name: "First"},
		calendar.Calendar{ID: sourceCalendarID(second), Name: "Second"},
	)

	removes := buttons(t, w, "Remove")
	if len(removes) != 2 {
		t.Fatalf("Remove controls = %d, want 2", len(removes))
	}
	test.Tap(removes[0])
	w.win.Close()

	if !reflect.DeepEqual(cfg.ICSSources, wantSources) {
		t.Fatalf("cancelled edit mutated caller sources: got %#v, want %#v", cfg.ICSSources, wantSources)
	}
}

type calendarLookupResult struct {
	calendars []calendar.Calendar
	err       error
}

type queuedApp struct {
	fyne.App
	driver fyne.Driver
}

func (a *queuedApp) Driver() fyne.Driver { return a.driver }

type queuedDriver struct {
	fyne.Driver
	tasks chan func()
}

func (d *queuedDriver) DoFromGoroutine(fn func(), wait bool) {
	if wait {
		fn()
		return
	}
	d.tasks <- fn
}

type testCalendarProvider struct {
	started   chan struct{}
	results   chan calendarLookupResult
	driver    *queuedDriver
	delivered bool
	drained   bool
}

func (p *testCalendarProvider) Name() string { return "test" }

func (p *testCalendarProvider) Calendars(ctx context.Context) ([]calendar.Calendar, error) {
	close(p.started)
	select {
	case result := <-p.results:
		return result.calendars, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *testCalendarProvider) Events(context.Context, time.Time, time.Time) ([]calendar.Event, error) {
	return nil, nil
}

func (p *testCalendarProvider) finish(t *testing.T, result calendarLookupResult) {
	t.Helper()
	if p.delivered {
		t.Fatal("calendar lookup already released")
	}
	p.delivered = true
	select {
	case p.results <- result:
	case <-time.After(time.Second):
		t.Fatal("calendar lookup did not finish")
	}
	p.drain(t)
}

func (p *testCalendarProvider) drain(t *testing.T) {
	t.Helper()
	select {
	case task := <-p.driver.tasks:
		task()
		p.drained = true
	case <-time.After(time.Second):
		t.Fatal("calendar lookup did not queue its UI update")
	}
	for {
		select {
		case task := <-p.driver.tasks:
			task()
		default:
			return
		}
	}
}

func (p *testCalendarProvider) cleanup(t *testing.T) {
	t.Helper()
	if p.drained {
		return
	}
	if !p.delivered {
		p.delivered = true
		select {
		case p.results <- calendarLookupResult{}:
		case <-time.After(time.Second):
			t.Error("calendar lookup did not finish during cleanup")
			return
		}
	}
	p.drain(t)
}

func isolatePrefsConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("AppData", dir)
}

func openTestWindow(t *testing.T, cfg config.Config, onSave func(config.Config)) (*Window, *testCalendarProvider) {
	t.Helper()
	previousApp := fyne.CurrentApp()
	baseApp := test.NewApp()
	driver := &queuedDriver{Driver: baseApp.Driver(), tasks: make(chan func())}
	app := &queuedApp{App: baseApp, driver: driver}
	fyne.SetCurrentApp(app)

	provider := &testCalendarProvider{
		started: make(chan struct{}),
		results: make(chan calendarLookupResult),
		driver:  driver,
	}
	t.Cleanup(func() {
		provider.cleanup(t)
		baseApp.Quit()
		fyne.SetCurrentApp(previousApp)
	})

	w := New(app, provider, onSave)
	w.show(cfg)
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("calendar lookup did not start")
	}
	return w, provider
}

func finishCalendarLookup(t *testing.T, w *Window, provider *testCalendarProvider, calendars ...calendar.Calendar) {
	t.Helper()
	provider.finish(t, calendarLookupResult{calendars: calendars})
	if len(calendars) > 0 && !hasLabel(w.win.Content(), calendars[0].Name) {
		t.Fatalf("calendar lookup did not apply %q", calendars[0].Name)
	}
}

func testSource(id string) config.ICSSource {
	return config.ICSSource{ID: id, URL: "https://calendar.example/" + id + ".ics"}
}

func sourceCalendarID(src config.ICSSource) string {
	return config.SourceICS + ":" + src.ID
}

func sourceChecks(w *Window) []*widget.Check {
	var checks []*widget.Check
	for _, object := range canvasObjects(w.win.Content()) {
		if check, ok := object.(*widget.Check); ok && check.Text == "Show" {
			checks = append(checks, check)
		}
	}
	return checks
}

func button(t *testing.T, w *Window, text string) *widget.Button {
	t.Helper()
	buttons := buttons(t, w, text)
	if len(buttons) != 1 {
		t.Fatalf("%q controls = %d, want 1", text, len(buttons))
	}
	return buttons[0]
}

func buttons(t *testing.T, w *Window, text string) []*widget.Button {
	t.Helper()
	var buttons []*widget.Button
	for _, object := range canvasObjects(w.win.Content()) {
		if button, ok := object.(*widget.Button); ok && button.Text == text {
			buttons = append(buttons, button)
		}
	}
	return buttons
}

func hasLabel(object fyne.CanvasObject, text string) bool {
	for _, object := range canvasObjects(object) {
		if label, ok := object.(*widget.Label); ok && label.Text == text {
			return true
		}
	}
	return false
}

func canvasObjects(root fyne.CanvasObject) []fyne.CanvasObject {
	var objects []fyne.CanvasObject
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		if object == nil {
			return
		}
		objects = append(objects, object)
		switch object := object.(type) {
		case *fyne.Container:
			for _, child := range object.Objects {
				visit(child)
			}
		case *container.Scroll:
			visit(object.Content)
		}
	}
	visit(root)
	return objects
}
