package overlay

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// baselineHeight is the screen height the type scale was designed against.
// Sizes are multiplied by (actual height / baseline) so the alert is the
// same physical proportion of a 1080p laptop panel and a 4K desktop.
const baselineHeight = 1080

// typeScale expresses each element as a fraction of screen height rather
// than a pixel size, which is what keeps the alert readable across displays.
var typeScale = struct {
	Kicker    float32
	Title     float32
	Countdown float32
	Subtitle  float32
}{
	Kicker:    0.022,
	Title:     0.058,
	Countdown: 0.150,
	Subtitle:  0.026,
}

// scaleFactor converts a canvas size into a multiplier for theme sizes,
// clamped so an unusual display cannot produce absurd widgets.
func scaleFactor(size fyne.Size) float32 {
	if size.Height <= 0 {
		return 1
	}
	factor := size.Height / baselineHeight
	if factor < 1 {
		return 1
	}
	if factor > 4 {
		return 4
	}
	return factor
}

// scaledTheme enlarges every theme dimension by a constant factor. Fyne
// sizes widget text from the theme, so this is what makes the buttons grow
// along with the headline type.
type scaledTheme struct {
	base   fyne.Theme
	factor float32
}

func newScaledTheme(factor float32) fyne.Theme {
	return &scaledTheme{base: theme.DefaultTheme(), factor: factor}
}

func (t *scaledTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.base.Size(name) * t.factor
}

func (t *scaledTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	// Colours pass through untouched: the alert paints its own background
	// and headline text, so only the buttons consult the theme and they
	// should follow the desktop's light/dark preference.
	return t.base.Color(name, variant)
}

func (t *scaledTheme) Font(style fyne.TextStyle) fyne.Resource { return t.base.Font(style) }

func (t *scaledTheme) Icon(name fyne.ThemeIconName) fyne.Resource { return t.base.Icon(name) }
