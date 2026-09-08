// Package overlay shows the full-screen meeting alert.
//
// This is the app's reason for existing: a tray label is easy to miss, so
// when a meeting is about to start the alert takes the whole screen, states
// the time remaining in type you can read from across the room, and offers a
// single obvious action.
//
// Fyne owns the main thread. Every exported method here is safe to call from
// any goroutine; each marshals onto the UI thread with fyne.Do.
package overlay

import (
	"fmt"
	"image/color"
	"log/slog"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
	"github.com/spriz/meeting-blaster/internal/screens"
)

// windowTitle is also how the window is located in the X11 tree in order to
// pin it to a monitor, so it must stay stable.
const windowTitle = "Meeting starting"

// The palette is deliberately high-contrast: this window has one job, which
// is to be noticed.
var (
	colorBackground = color.NRGBA{R: 0x0d, G: 0x0d, B: 0x12, A: 0xff}
	colorUrgent     = color.NRGBA{R: 0xff, G: 0x53, B: 0x69, A: 0xff}
	colorPrimary    = color.NRGBA{R: 0xf5, G: 0xf5, B: 0xfa, A: 0xff}
	colorMuted      = color.NRGBA{R: 0x9a, G: 0x9a, B: 0xb0, A: 0xff}
)

// Options tunes a single alert.
type Options struct {
	// Timeout auto-dismisses the alert. Zero leaves it up until the user
	// acts, which is the default: an alert that vanishes on its own is an
	// alert you can miss.
	Timeout time.Duration

	// TimeLayout formats the meeting's start and end times.
	TimeLayout string

	// Monitors is a config.Monitors* value selecting which displays the
	// alert covers.
	Monitors string

	// OnJoin is called when the user chooses to join.
	OnJoin func(calendar.Event)
}

// Controller shows alerts on a Fyne app. Construct one and reuse it.
type Controller struct {
	app fyne.App
	log *slog.Logger

	mu      sync.Mutex
	windows []*alertUI
	stop    chan struct{}
}

// New returns a Controller drawing on the given app.
func New(app fyne.App, log *slog.Logger) *Controller {
	if log == nil {
		log = slog.Default()
	}
	return &Controller{app: app, log: log}
}

// Show raises the alert for ev, replacing any alert already up. Safe to call
// from any goroutine.
func (c *Controller) Show(ev calendar.Event, opts Options) {
	fyne.Do(func() { c.show(ev, opts) })
}

// Dismiss closes the current alert on every display. Safe to call from any
// goroutine.
func (c *Controller) Dismiss() {
	fyne.Do(func() { c.close() })
}

func (c *Controller) show(ev calendar.Event, opts Options) {
	c.close()

	if opts.TimeLayout == "" {
		opts.TimeLayout = "15:04"
	}

	targets := c.targets(opts.Monitors)
	stop := make(chan struct{})

	var built []*alertUI
	for _, target := range targets {
		ui := c.buildWindow(ev, opts, target)
		if ui != nil {
			built = append(built, ui)
		}
	}

	c.mu.Lock()
	c.windows, c.stop = built, stop
	c.mu.Unlock()

	go c.runCountdown(ev, built, stop, opts.Timeout)
}

// target is one display to cover. A nil monitor means "wherever the window
// manager puts it", which is the fallback when enumeration is unavailable.
type target struct {
	monitor *screens.Monitor
}

// targets resolves the configured mode into the displays to cover. Any
// failure degrades to a single window placed by the window manager, because
// an alert in the wrong place still beats no alert.
func (c *Controller) targets(mode string) []target {
	switch mode {
	case config.MonitorsActive:
		return []target{{}}

	case config.MonitorsAll:
		monitors, err := screens.List()
		if err != nil || len(monitors) == 0 {
			c.log.Debug("cannot enumerate monitors, using one window", "error", err)
			return []target{{}}
		}
		out := make([]target, 0, len(monitors))
		for i := range monitors {
			out = append(out, target{monitor: &monitors[i]})
		}
		return out

	default: // config.MonitorsPrimary
		monitor, err := screens.Primary()
		if err != nil {
			c.log.Debug("cannot find primary monitor, using one window", "error", err)
			return []target{{}}
		}
		return []target{{monitor: &monitor}}
	}
}

// buildWindow creates, shows, and pins one alert window.
func (c *Controller) buildWindow(ev calendar.Event, opts Options, t target) *alertUI {
	// Note which windows already carry our title, so the one created below
	// can be told apart from alerts already on other displays.
	var existing map[uint32]bool
	if t.monitor != nil {
		existing = make(map[uint32]bool)
		if ids, err := screens.Find(windowTitle); err == nil {
			for _, id := range ids {
				existing[id] = true
			}
		}
	}

	win := c.app.NewWindow(windowTitle)
	win.SetFullScreen(true)
	win.SetPadded(false)

	background := canvas.NewRectangle(colorBackground)

	kicker := canvas.NewText("MEETING STARTING", colorUrgent)
	kicker.TextSize = baselineHeight * typeScale.Kicker
	kicker.TextStyle = fyne.TextStyle{Bold: true}
	kicker.Alignment = fyne.TextAlignCenter

	title := canvas.NewText(ev.Title, colorPrimary)
	title.TextSize = baselineHeight * typeScale.Title
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	countdown := canvas.NewText(countdownText(ev, time.Now()), colorUrgent)
	countdown.TextSize = baselineHeight * typeScale.Countdown
	countdown.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	countdown.Alignment = fyne.TextAlignCenter

	when := fmt.Sprintf("%s – %s",
		ev.Start.Format(opts.TimeLayout), ev.End.Format(opts.TimeLayout))
	if ev.Location != "" && !ev.HasLink() {
		when += "   ·   " + ev.Location
	}
	subtitle := canvas.NewText(when, colorMuted)
	subtitle.TextSize = baselineHeight * typeScale.Subtitle
	subtitle.Alignment = fyne.TextAlignCenter

	dismiss := widget.NewButton("Dismiss  (Esc)", func() { c.Dismiss() })

	buttons := container.NewHBox(layoutSpacer(), dismiss, layoutSpacer())
	if ev.HasLink() {
		join := widget.NewButton("Join now", func() {
			if opts.OnJoin != nil {
				opts.OnJoin(ev)
			}
			c.Dismiss()
		})
		join.Importance = widget.HighImportance
		buttons = container.NewHBox(layoutSpacer(), join, dismiss, layoutSpacer())
	}

	content := container.NewVBox(
		layoutSpacer(),
		kicker,
		spacerOf(12),
		title,
		spacerOf(8),
		countdown,
		spacerOf(8),
		subtitle,
		spacerOf(40),
		buttons,
		layoutSpacer(),
	)

	win.SetContent(container.NewStack(background, container.NewCenter(content)))

	win.Canvas().SetOnTypedKey(func(key *fyne.KeyEvent) {
		switch key.Name {
		case fyne.KeyEscape:
			c.Dismiss()
		case fyne.KeyReturn, fyne.KeyEnter:
			if ev.HasLink() && opts.OnJoin != nil {
				opts.OnJoin(ev)
			}
			c.Dismiss()
		}
	})

	// Closing any one window dismisses the whole alert, so the displays do
	// not get out of step.
	win.SetOnClosed(func() { c.Dismiss() })

	win.Show()
	win.RequestFocus()

	ui := &alertUI{
		win: win, kicker: kicker, title: title,
		countdown: countdown, subtitle: subtitle,
		monitor: t.monitor,
	}

	c.pin(ui, existing)

	// The canvas reports its real size only once the window is mapped, and
	// buildWindow already runs on the UI thread, so this waits a frame and
	// then marshals back rather than measuring a zero-sized canvas.
	time.AfterFunc(50*time.Millisecond, func() {
		fyne.Do(func() { c.applyScale(ui) })
	})
	return ui
}

// pin moves the newly created window onto its monitor. The window manager
// owns the geometry of fullscreen windows, so this goes through the EWMH
// message meant for the purpose rather than setting coordinates.
func (c *Controller) pin(ui *alertUI, existing map[uint32]bool) {
	if ui.monitor == nil {
		return
	}

	ids, err := screens.Find(windowTitle)
	if err != nil {
		c.log.Debug("cannot locate alert window to pin", "error", err)
		return
	}
	for _, id := range ids {
		if existing[id] {
			continue
		}
		if err := screens.PinFullscreen(id, *ui.monitor); err != nil {
			c.log.Debug("cannot pin alert window", "monitor", ui.monitor.Name, "error", err)
			return
		}
		ui.windowID = id
		c.log.Debug("alert pinned", "monitor", ui.monitor.Name, "window", id)
		return
	}
}

// runCountdown updates the big number on every display once a second, until
// the alert is dismissed or times out.
func (c *Controller) runCountdown(ev calendar.Event, uis []*alertUI, stop <-chan struct{}, timeout time.Duration) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}

	for {
		select {
		case <-stop:
			return
		case <-deadline:
			c.Dismiss()
			return
		case now := <-ticker.C:
			// Once the meeting has been running for a while the alert has
			// served its purpose; leaving it up would block the screen.
			if now.Sub(ev.Start) > 2*time.Minute {
				c.Dismiss()
				return
			}
			label := countdownText(ev, now)
			fyne.Do(func() {
				for _, ui := range uis {
					ui.countdown.Text = label
					ui.countdown.Refresh()
					c.applyScale(ui)
				}
			})
		}
	}
}

func (c *Controller) close() {
	c.mu.Lock()
	windows, stop := c.windows, c.stop
	c.windows, c.stop = nil, nil
	c.mu.Unlock()

	if stop != nil {
		safeClose(stop)
	}
	for _, ui := range windows {
		ui.win.SetOnClosed(nil)
		ui.win.Close()
	}
}

// countdownText renders the time remaining, switching to a "now" message once
// the meeting has actually begun.
func countdownText(ev calendar.Event, now time.Time) string {
	remaining := ev.Start.Sub(now).Round(time.Second)
	if remaining <= 0 {
		return "NOW"
	}
	minutes := int(remaining / time.Minute)
	seconds := int((remaining % time.Minute) / time.Second)
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func spacerOf(height float32) fyne.CanvasObject {
	s := canvas.NewRectangle(color.Transparent)
	s.SetMinSize(fyne.NewSize(1, height))
	return s
}

func layoutSpacer() fyne.CanvasObject {
	s := canvas.NewRectangle(color.Transparent)
	s.SetMinSize(fyne.NewSize(theme.Padding()*2, 1))
	return s
}

func safeClose(ch chan struct{}) {
	defer func() { _ = recover() }() // already closed
	close(ch)
}

// alertUI groups one window's text nodes, whose size depends on the display.
type alertUI struct {
	win       fyne.Window
	kicker    *canvas.Text
	title     *canvas.Text
	countdown *canvas.Text
	subtitle  *canvas.Text

	monitor  *screens.Monitor
	windowID uint32

	lastHeight float32
}

// applyScale resizes the headline type to the actual screen and enlarges the
// widget theme to match. Fyne reports a scale of 1 under XWayland even on a
// HiDPI panel, so without this the alert renders as a small block in the
// middle of a 4K screen - readable, but easy to miss, which is the one thing
// this window must never be.
//
// Must be called on the UI thread.
func (c *Controller) applyScale(ui *alertUI) {
	size := ui.win.Canvas().Size()
	if size.Height <= 0 || size.Height == ui.lastHeight {
		return
	}
	ui.lastHeight = size.Height

	c.app.Settings().SetTheme(newScaledTheme(scaleFactor(size)))

	ui.kicker.TextSize = size.Height * typeScale.Kicker
	ui.title.TextSize = size.Height * typeScale.Title
	ui.countdown.TextSize = size.Height * typeScale.Countdown
	ui.subtitle.TextSize = size.Height * typeScale.Subtitle

	for _, t := range []*canvas.Text{ui.kicker, ui.title, ui.countdown, ui.subtitle} {
		t.Refresh()
	}
}
