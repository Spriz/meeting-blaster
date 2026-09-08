// Package overlay shows the full-screen meeting alert.
//
// This is the app's reason for existing: a tray label is easy to miss, so
// when a meeting is about to start the alert takes the whole screen, states
// the time remaining in type you can read from across the room, and offers a
// single obvious action.
//
// Fyne owns the main thread. Every method here is safe to call from any
// goroutine; each marshals onto the UI thread with fyne.Do.
package overlay

import (
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spriz/meeting-blaster/internal/calendar"
)

// Palette is deliberately high-contrast: this window has one job, which is
// to be noticed.
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

	// OnJoin is called when the user clicks Join. It receives the event so
	// the caller can open the link with its own browser settings.
	OnJoin func(calendar.Event)
}

// Controller shows alerts on a Fyne app. Construct one and reuse it.
type Controller struct {
	app fyne.App

	mu      sync.Mutex
	current fyne.Window
	stop    chan struct{}
}

// New returns a Controller drawing on the given app.
func New(app fyne.App) *Controller { return &Controller{app: app} }

// Show raises a full-screen alert for ev, replacing any alert already up.
// Safe to call from any goroutine.
func (c *Controller) Show(ev calendar.Event, opts Options) {
	fyne.Do(func() { c.show(ev, opts) })
}

// Dismiss closes the current alert, if any. Safe to call from any goroutine.
func (c *Controller) Dismiss() {
	fyne.Do(func() { c.close() })
}

func (c *Controller) show(ev calendar.Event, opts Options) {
	c.close()

	if opts.TimeLayout == "" {
		opts.TimeLayout = "15:04"
	}

	win := c.app.NewWindow("Meeting starting")
	win.SetFullScreen(true)
	win.SetPadded(false)

	stop := make(chan struct{})

	c.mu.Lock()
	c.current, c.stop = win, stop
	c.mu.Unlock()

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

	// Escape is the muscle-memory dismissal for a full-screen window.
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

	win.SetOnClosed(func() {
		c.mu.Lock()
		if c.current == win {
			c.current, c.stop = nil, nil
		}
		c.mu.Unlock()
		safeClose(stop)
	})

	win.Show()
	win.RequestFocus()

	ui := &alertUI{win: win, kicker: kicker, title: title, countdown: countdown, subtitle: subtitle}

	// The canvas reports its real size only once the window is mapped, so
	// the first measurement happens just after Show rather than before it.
	fyne.Do(func() { c.applyScale(ui) })

	go c.runCountdown(ev, ui, stop, opts.Timeout)
}

// runCountdown updates the big number once a second until the alert is
// dismissed or times out. It runs off the UI thread and marshals each update.
func (c *Controller) runCountdown(ev calendar.Event, ui *alertUI, stop <-chan struct{}, timeout time.Duration) {
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
				ui.countdown.Text = label
				ui.countdown.Refresh()
				// Re-check in case the window landed on a different
				// monitor, or the size was not final on the first frame.
				c.applyScale(ui)
			})
		}
	}
}

func (c *Controller) close() {
	c.mu.Lock()
	win, stop := c.current, c.stop
	c.current, c.stop = nil, nil
	c.mu.Unlock()

	if stop != nil {
		safeClose(stop)
	}
	if win != nil {
		win.SetOnClosed(nil)
		win.Close()
	}
}

// countdownText renders the time remaining, switching to a "now" message
// once the meeting has actually begun.
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

// alertUI groups the text nodes whose size depends on the display.
type alertUI struct {
	win       fyne.Window
	kicker    *canvas.Text
	title     *canvas.Text
	countdown *canvas.Text
	subtitle  *canvas.Text

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
