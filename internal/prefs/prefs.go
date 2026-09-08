// Package prefs is the settings window: calendar selection, clock format,
// and how far ahead the alerts fire.
package prefs

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/spriz/meeting-blaster/internal/calendar"
	"github.com/spriz/meeting-blaster/internal/config"
)

// Window shows and saves settings. One instance is reused; opening it twice
// raises the existing window rather than stacking copies.
type Window struct {
	app      fyne.App
	provider calendar.Provider

	// OnSave receives the edited config. It is called on the UI thread.
	OnSave func(config.Config)

	mu  sync.Mutex
	win fyne.Window
}

// New builds a settings window bound to a provider, which is queried for the
// calendar list when the window opens.
func New(app fyne.App, provider calendar.Provider, onSave func(config.Config)) *Window {
	return &Window{app: app, provider: provider, OnSave: onSave}
}

// Show opens the window, seeded with cfg. Safe to call from any goroutine.
func (w *Window) Show(cfg config.Config) {
	fyne.Do(func() { w.show(cfg) })
}

func (w *Window) show(cfg config.Config) {
	w.mu.Lock()
	existing := w.win
	w.mu.Unlock()

	if existing != nil {
		existing.RequestFocus()
		return
	}

	win := w.app.NewWindow("meeting-blaster preferences")
	win.Resize(fyne.NewSize(560, 760))

	w.mu.Lock()
	w.win = win
	w.mu.Unlock()

	win.SetOnClosed(func() {
		w.mu.Lock()
		w.win = nil
		w.mu.Unlock()
	})

	edited := cfg

	// --- alerting -------------------------------------------------------
	alertEntry := widget.NewEntry()
	alertEntry.SetText(strconv.Itoa(int(cfg.AlertLead.Std().Minutes())))

	notifyEntry := widget.NewEntry()
	notifyEntry.SetText(strconv.Itoa(int(cfg.NotifyLead.Std().Minutes())))

	timeoutEntry := widget.NewEntry()
	timeoutEntry.SetText(strconv.Itoa(int(cfg.OverlayTimeout.Std().Seconds())))

	// Which displays the alert blocks. Labels are phrased around intent
	// rather than the stored values.
	monitorLabels := map[string]string{
		config.MonitorsPrimary: "Primary display only",
		config.MonitorsAll:     "Every display",
		config.MonitorsActive:  "Wherever the window opens",
	}
	monitorValues := map[string]string{}
	var monitorOptions []string
	for _, mode := range config.ValidMonitorModes {
		monitorOptions = append(monitorOptions, monitorLabels[mode])
		monitorValues[monitorLabels[mode]] = mode
	}
	monitorChoice := widget.NewRadioGroup(monitorOptions, func(choice string) {
		if mode, ok := monitorValues[choice]; ok {
			edited.OverlayMonitors = mode
		}
	})
	monitorChoice.SetSelected(monitorLabels[cfg.MonitorMode()])

	// --- display --------------------------------------------------------
	clock := widget.NewRadioGroup([]string{"24-hour", "12-hour"}, func(choice string) {
		edited.Use24Hour = choice == "24-hour"
	})
	if cfg.Use24Hour {
		clock.SetSelected("24-hour")
	} else {
		clock.SetSelected("12-hour")
	}
	clock.Horizontal = true

	titleLen := widget.NewEntry()
	titleLen.SetText(strconv.Itoa(cfg.TitleMaxLen))

	hideDeclined := widget.NewCheck("Hide meetings I declined", func(v bool) {
		edited.HideDeclined = v
	})
	hideDeclined.SetChecked(cfg.HideDeclined)

	browserEntry := widget.NewEntry()
	browserEntry.SetPlaceHolder("system default")
	browserEntry.SetText(cfg.JoinBrowser)

	// --- calendars ------------------------------------------------------
	// Loaded asynchronously: the window must open instantly even if the
	// network is slow or the token needs refreshing.
	calendarBox := container.NewVBox(widget.NewLabel("Loading calendars…"))
	selected := make(map[string]bool, len(cfg.CalendarIDs))
	for _, id := range cfg.CalendarIDs {
		selected[id] = true
	}

	go w.loadCalendars(calendarBox, selected)

	status := widget.NewLabel("")

	save := widget.NewButton("Save", func() {
		alert, err := minutes(alertEntry.Text)
		if err != nil {
			status.SetText("Alert lead: " + err.Error())
			return
		}
		notify, err := minutes(notifyEntry.Text)
		if err != nil {
			status.SetText("Notification lead: " + err.Error())
			return
		}
		timeout, err := seconds(timeoutEntry.Text)
		if err != nil {
			status.SetText("Overlay timeout: " + err.Error())
			return
		}
		length, err := strconv.Atoi(titleLen.Text)
		if err != nil || length < 5 {
			status.SetText("Title length must be a number of at least 5")
			return
		}

		edited.AlertLead = config.Duration(alert)
		edited.NotifyLead = config.Duration(notify)
		edited.OverlayTimeout = config.Duration(timeout)
		edited.TitleMaxLen = length
		edited.JoinBrowser = browserEntry.Text
		edited.CalendarIDs = selectedIDs(selected)

		if err := config.Save(edited); err != nil {
			status.SetText("Could not save: " + err.Error())
			return
		}
		if w.OnSave != nil {
			w.OnSave(edited)
		}
		win.Close()
	})
	save.Importance = widget.HighImportance

	form := container.NewVBox(
		widget.NewLabelWithStyle("Alerts", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		labelled("Full-screen alert (minutes before)", alertEntry),
		labelled("Notification (minutes before, 0 = off)", notifyEntry),
		labelled("Auto-dismiss overlay after (seconds, 0 = never)", timeoutEntry),
		widget.NewLabel("Block which displays:"),
		monitorChoice,
		widget.NewSeparator(),

		widget.NewLabelWithStyle("Display", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		labelled("Clock", clock),
		labelled("Max title length in tray", titleLen),
		labelled("Browser for join links", browserEntry),
		hideDeclined,
		widget.NewSeparator(),

		widget.NewLabelWithStyle("Calendars", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Unticking everything watches all calendars."),
	)

	// The settings and the calendar list scroll together as one column.
	// Putting the list in the centre of a Border layout gave it only the
	// space the form left over, which on a full calendar account was a
	// couple of pixels.
	// Padding keeps the entry fields clear of the scrollbar, which
	// otherwise sits on top of their right edge.
	scrolling := container.NewVScroll(
		container.NewPadded(container.NewVBox(form, calendarBox)),
	)

	content := container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), status, save),
		nil, nil,
		scrolling,
	)

	win.SetContent(content)
	win.Show()

	// Fyne scrolls to whichever widget takes focus first, which lands the
	// window partway down the settings. Start at the top instead.
	scrolling.ScrollToTop()
}

// loadCalendars fetches the calendar list off the UI thread, then swaps the
// placeholder for real checkboxes.
func (w *Window) loadCalendars(box *fyne.Container, selected map[string]bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cals, err := w.provider.Calendars(ctx)

	fyne.Do(func() {
		box.RemoveAll()
		if err != nil {
			box.Add(widget.NewLabel("Could not load calendars:\n" + err.Error()))
			box.Refresh()
			return
		}
		for _, cal := range cals {
			id, name := cal.ID, cal.Name
			if cal.Primary {
				name += "  (primary)"
			}
			check := widget.NewCheck(name, func(v bool) { selected[id] = v })
			check.SetChecked(selected[id])
			box.Add(check)
		}
		box.Refresh()
	})
}

func labelled(text string, w fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil, widget.NewLabel(text), nil, w)
}

func selectedIDs(selected map[string]bool) []string {
	var out []string
	for id, on := range selected {
		if on {
			out = append(out, id)
		}
	}
	return out
}

func minutes(s string) (time.Duration, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("must be a whole number of minutes")
	}
	return time.Duration(n) * time.Minute, nil
}

func seconds(s string) (time.Duration, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("must be a whole number of seconds")
	}
	return time.Duration(n) * time.Second, nil
}
