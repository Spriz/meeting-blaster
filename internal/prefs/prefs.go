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
	// network is slow.
	calendarBox := container.NewVBox(widget.NewLabel("Loading calendars…"))
	calendarNames := make(map[string]string)
	selected := make(map[string]bool, len(cfg.CalendarIDs))
	for _, id := range cfg.CalendarIDs {
		selected[id] = true
	}

	status := widget.NewLabel("")

	// --- calendar subscriptions -----------------------------------------
	// Edited in place on `edited`, which Save persists and hands to
	// OnSave; the live ICS provider picks the list up from there.
	sourceBox := container.NewVBox()

	var redrawSources func()
	redrawSources = func() {
		sourceBox.RemoveAll()
		if len(edited.ICSSources) == 0 {
			sourceBox.Add(widget.NewLabel("No subscriptions yet."))
		}
		for _, src := range edited.ICSSources {
			id := src.ID
			name := subscriptionLabel(src, calendarNames)
			remove := widget.NewButton("Remove", func() {
				for i := range edited.ICSSources {
					if edited.ICSSources[i].ID == id {
						edited.ICSSources = append(edited.ICSSources[:i], edited.ICSSources[i+1:]...)
						break
					}
				}
				// Save writes back selectedIDs(selected), so the
				// watch entry has to go too; otherwise it lingers
				// pointing at a subscription that no longer exists.
				delete(selected, config.SourceICS+":"+id)
				redrawSources()
				sourceBox.Refresh()
			})
			sourceBox.Add(container.NewBorder(nil, nil, nil, remove, widget.NewLabel(name)))
		}
	}

	urlEntry := widget.NewPasswordEntry()
	urlEntry.SetPlaceHolder("webcal://… , https://….ics or /path/to/calendar.ics")

	addSource := widget.NewButton("Add", func() {
		url, err := config.NormalizeCalendarURL(urlEntry.Text)
		if err != nil {
			status.SetText(err.Error())
			return
		}
		id := config.SourceID(url)
		for _, src := range edited.ICSSources {
			if src.ID == id {
				status.SetText("Already subscribed to that calendar.")
				return
			}
		}
		edited.ICSSources = append(edited.ICSSources, config.ICSSource{ID: id, URL: url})
		// An empty selection already means "watch everything", so
		// seeding it here would narrow the watch list to this one feed.
		if len(selected) > 0 {
			selected[config.SourceICS+":"+id] = true
		}
		status.SetText("")
		urlEntry.SetText("")
		redrawSources()
		sourceBox.Refresh()
	})

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

		widget.NewLabelWithStyle("Calendar subscriptions", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		sourceBox,
		container.NewBorder(nil, nil, nil, addSource, urlEntry),
		widget.NewLabel("Subscriptions appear in the calendar list below after saving and restarting."),
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

	redrawSources()
	go w.loadCalendars(calendarBox, selected, func(cals []calendar.Calendar) {
		for _, cal := range cals {
			calendarNames[cal.ID] = cal.Name
		}
		redrawSources()
		sourceBox.Refresh()
	})

	win.SetContent(content)
	win.Show()

	// Fyne scrolls to whichever widget takes focus first, which lands the
	// window partway down the settings. Start at the top instead.
	scrolling.ScrollToTop()
}

// loadCalendars fetches the calendar list off the UI thread, then swaps the
// placeholder for real checkboxes and supplies subscription names on the UI thread.
func (w *Window) loadCalendars(box *fyne.Container, selected map[string]bool, onLoaded func([]calendar.Calendar)) {
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
		onLoaded(cals)
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

func subscriptionLabel(src config.ICSSource, calendarNames map[string]string) string {
	if src.Name != "" {
		return src.Name
	}
	if name := calendarNames[config.SourceICS+":"+src.ID]; name != "" {
		return name
	}
	return "Calendar " + src.ID
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
