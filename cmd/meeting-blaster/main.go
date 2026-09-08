// Command meeting-blaster puts your next meeting in the system tray and
// throws a full-screen alert at you before it starts.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/systray"

	"github.com/spriz/meeting-blaster/internal/browser"
	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/calendar/ics"
	"github.com/spriz/meeting-blaster/internal/calendar/multi"
	"github.com/spriz/meeting-blaster/internal/config"
	"github.com/spriz/meeting-blaster/internal/engine"
	"github.com/spriz/meeting-blaster/internal/notify"
	"github.com/spriz/meeting-blaster/internal/overlay"
	"github.com/spriz/meeting-blaster/internal/prefs"
	"github.com/spriz/meeting-blaster/internal/screens"
	"github.com/spriz/meeting-blaster/internal/singleton"
	"github.com/spriz/meeting-blaster/internal/tray"
)

// appID identifies the app to the desktop and to Fyne's preference store.
const appID = "com.github.spriz.meeting-blaster"

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print the version and exit")
		verbose     = flag.Bool("v", false, "log at debug level")
		testAlert   = flag.Bool("test-alert", false, "show a sample full-screen alert and exit")
		listScreens = flag.Bool("list-monitors", false, "list connected monitors and exit")
		addCalendar = flag.String("add-calendar", "", "subscribe to an iCalendar feed (webcal:// or https:// URL, or a path to a .ics file) and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("meeting-blaster", version)
		return
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	if *listScreens {
		if err := printMonitors(); err != nil {
			log.Error("cannot list monitors", "error", err)
			os.Exit(1)
		}
		return
	}

	if *addCalendar != "" {
		if err := addICSSource(*addCalendar); err != nil {
			log.Error("cannot add calendar", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(log, *testAlert); err != nil {
		// Setup guidance has already been printed in full; repeating it
		// as a one-line error adds noise and no information.
		if !errors.Is(err, errSetupPrinted) {
			log.Error("fatal", "error", err)
		}
		os.Exit(1)
	}
}

func run(log *slog.Logger, testAlert bool) error {
	// A second copy would mean two tray icons and two full-screen alerts,
	// which is worse than none.
	release, err := singleton.Acquire()
	if err != nil {
		var running singleton.ErrAlreadyRunning
		if errors.As(err, &running) {
			fmt.Println("meeting-blaster is already running.")
			return nil
		}
		return err
	}
	defer release()

	cfg, err := config.Load()
	if err != nil {
		// A malformed config should not brick the app; fall back to
		// defaults and say so.
		log.Warn("using default settings", "error", err)
		cfg = config.Default()
	}

	if testAlert {
		ui := fyneapp.NewWithID(appID)
		overlays := overlay.New(ui, log)
		return runTestAlert(ui, overlays, cfg)
	}

	var sources []multi.Source
	icsProvider := ics.New(icsSources(cfg), log)
	if len(cfg.ICSSources) > 0 {
		sources = append(sources, multi.Source{Key: config.SourceICS, Provider: icsProvider})
	}
	if len(sources) == 0 {
		return setupInstructions()
	}
	provider := multi.New(log, sources...)

	// The Fyne app must be created on the main goroutine, before anything
	// tries to show a window.
	ui := fyneapp.NewWithID(appID)
	overlays := overlay.New(ui, log)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var eng *engine.Engine

	openMeeting := func(ev calendar.Event) {
		if !ev.HasLink() {
			log.Warn("meeting has no join link", "title", ev.Title)
			_ = notify.Send(ev.Title, "This meeting has no join link.")
			return
		}
		if err := browser.Open(ev.MeetingURL, eng.Config().JoinBrowser); err != nil {
			log.Error("could not open meeting link", "error", err)
			_ = notify.Send("Could not open meeting", err.Error())
		}
	}

	settings := prefs.New(ui, provider, func(updated config.Config) {
		icsProvider.SetSources(icsSources(updated))
		eng.SetConfig(updated) // already triggers an immediate Refresh
		log.Info("settings saved")
	})

	trayUI := tray.New(tray.Actions{
		OnJoin:        openMeeting,
		OnRefresh:     func() { eng.Refresh() },
		OnPreferences: func() { settings.Show(eng.Config()) },
		OnQuit:        func() { cancel() },
	})

	eng = engine.New(provider, cfg, engine.Callbacks{
		OnState: func(state engine.State) {
			trayUI.Update(state, eng.Config(), time.Now())
		},
		OnAlert: func(ev calendar.Event) {
			log.Info("full-screen alert", "title", ev.Title, "start", ev.Start)
			overlays.Show(ev, overlay.Options{
				Timeout:    eng.Config().OverlayTimeout.Std(),
				TimeLayout: eng.Config().TimeLayout(),
				Monitors:   eng.Config().MonitorMode(),
				OnJoin:     openMeeting,
			})
		},
		OnNotify: func(ev calendar.Event) {
			body := fmt.Sprintf("Starts at %s", ev.Start.Format(eng.Config().TimeLayout()))
			if err := notify.Send(ev.Title, body); err != nil {
				log.Debug("notification failed", "error", err)
			}
		},
	}, log)

	go eng.Run(ctx)

	// systray runs alongside Fyne rather than owning the process: Fyne
	// needs the main thread for its own event loop.
	startTray, stopTray := systray.RunWithExternalLoop(trayUI.Ready, func() {})
	startTray()

	// Fyne quits when its last window closes. Our external systray does
	// not count, so retain a never-shown window for the background app.
	// Alerts and preferences can then close normally; Quit still stops
	// the loop explicitly. Keep this out of the one-shot --test-alert path.
	ui.NewWindow("meeting-blaster background")

	// Quitting from the tray cancels ctx; translate that into stopping the
	// Fyne loop, which is what actually ends the process.
	go func() {
		<-ctx.Done()
		stopTray()
		fyne.Do(func() { ui.Quit() })
	}()

	log.Info("meeting-blaster started", "version", version)
	ui.Run()
	return nil
}

// icsSources maps the saved subscriptions onto the provider's own type.
func icsSources(cfg config.Config) []ics.Source {
	out := make([]ics.Source, 0, len(cfg.ICSSources))
	for _, src := range cfg.ICSSources {
		out = append(out, ics.Source{
			ID:    src.ID,
			Name:  src.Name,
			URL:   src.URL,
			Email: src.Email,
		})
	}
	return out
}

// addICSSource appends a subscription to the saved config.
func addICSSource(raw string) error {
	url, err := config.NormalizeCalendarURL(raw)
	if err != nil {
		return errors.New("invalid calendar subscription: expected webcal://, https://, or a path to a .ics file")
	}
	id := config.SourceID(url)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	for _, src := range cfg.ICSSources {
		if src.ID == id {
			return fmt.Errorf("already subscribed to %s", calendarLabel(src))
		}
	}

	cfg.ICSSources = append(cfg.ICSSources, config.ICSSource{ID: id, URL: url})
	cfg = cfg.Watch(config.SourceICS + ":" + id)
	if err := config.Save(cfg); err != nil {
		return err
	}

	fmt.Printf("Subscribed to %s. Restart meeting-blaster to pick it up.\n", calendarLabel(config.ICSSource{ID: id, URL: url}))
	return nil
}

func calendarLabel(src config.ICSSource) string {
	if src.Name != "" {
		return src.Name
	}
	return "iCalendar subscription"
}

// runTestAlert shows a sample overlay so the alert can be checked without
// waiting for a real meeting.
func runTestAlert(ui fyne.App, overlays *overlay.Controller, cfg config.Config) error {
	sample := calendar.Event{
		Title:      "Sample meeting",
		Start:      time.Now().Add(45 * time.Second),
		End:        time.Now().Add(45*time.Second + 30*time.Minute),
		MeetingURL: "https://meet.google.com/abc-defg-hij",
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		overlays.Show(sample, overlay.Options{
			TimeLayout: cfg.TimeLayout(),
			Monitors:   cfg.MonitorMode(),
			OnJoin:     func(calendar.Event) {},
		})
	}()
	ui.Run()
	return nil
}

// printMonitors lists the displays, so a user can see what the
// overlay_monitors setting will act on.
func printMonitors() error {
	monitors, err := screens.List()
	if err != nil {
		return err
	}
	if len(monitors) == 0 {
		fmt.Println("No monitors detected.")
		return nil
	}
	fmt.Printf("%-4s %-12s %-20s %s\n", "IDX", "NAME", "GEOMETRY", "")
	for _, m := range monitors {
		note := ""
		if m.Primary {
			note = "primary"
		}
		fmt.Printf("%-4d %-12s %-20s %s\n", m.Index, m.Name,
			fmt.Sprintf("%dx%d+%d+%d", m.Width, m.Height, m.X, m.Y), note)
	}
	fmt.Println()
	fmt.Println(`Set "overlay_monitors" in config.json to "primary", "all", or "active".`)
	return nil
}

// setupInstructions explains how to add an iCalendar subscription.
func setupInstructions() error {
	fmt.Fprint(os.Stderr, `meeting-blaster has no calendar configured yet.

Subscribe to an iCalendar feed. No account is needed:

  meeting-blaster --add-calendar "webcal://example.com/your-calendar.ics"
  meeting-blaster --add-calendar "https://example.com/your-calendar.ics"

Most calendar services publish a private feed URL:
  Google Calendar  Settings -> your calendar -> "Secret address in iCal format"
  Outlook / M365   Settings -> Calendar -> Shared calendars -> Publish
  Nextcloud        Calendar -> ... -> Copy subscription link

For a local calendar file, pass its path instead:
  meeting-blaster --add-calendar "/path/to/calendar.ics"

Published feeds are refreshed by the provider on its own schedule, often
only every few hours, so a meeting added this morning may not appear today.

Then run meeting-blaster again.
`)
	return errSetupPrinted
}

// errSetupPrinted signals that setup guidance was written to stderr, so the
// caller should exit non-zero without logging anything further.
var errSetupPrinted = errors.New("no calendar configured")
