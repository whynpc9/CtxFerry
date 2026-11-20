package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// Fonts are vendored so that every machine renders the same glyphs.
// Sources and SHA-256 digests are listed in README.md.

//go:embed fonts/Inter-Variable.ttf
var interFont []byte

//go:embed fonts/JetBrainsMono-Variable.ttf
var monoFont []byte

const (
	fontUI   = "Inter"
	fontMono = "JetBrains Mono"
)

func registerFonts() {
	if err := ui.RegisterFont(interFont, fontUI); err != nil {
		panic(err)
	}
	if err := ui.RegisterFont(monoFont, fontMono); err != nil {
		panic(err)
	}
}

// Design tokens. Every colour used by the fixture comes from here.
type palette struct {
	Canvas       ui.Color // sidebar and window chrome
	Page         ui.Color // scrolling page behind cards
	Surface      ui.Color // top bar, cards
	Sunken       ui.Color // code, key-value wells, table headers
	Border       ui.Color
	BorderStrong ui.Color
	Text         ui.Color
	Muted        ui.Color
	Subtle       ui.Color

	Accent      ui.Color // navigation, primary action, links
	AccentHover ui.Color
	AccentSoft  ui.Color
	AccentText  ui.Color

	// Status families. Each has a strong (icon, text) and a soft (fill) tone.
	Pending, PendingSoft   ui.Color // waiting / awaiting publication
	Conflict, ConflictSoft ui.Color // conflicting revisions
	Blocked, BlockedSoft   ui.Color // blocked, failed, destructive
	Ok, OkSoft             ui.Color // confirmed, verified, reachable
	Unknown, UnknownSoft   ui.Color // unknown, stale, not checked
}

var tk = palette{
	Canvas:       ui.Hex("#F3F4F6"),
	Page:         ui.Hex("#F8F9FB"),
	Surface:      ui.Hex("#FFFFFF"),
	Sunken:       ui.Hex("#F7F8FA"),
	Border:       ui.Hex("#E2E5E9"),
	BorderStrong: ui.Hex("#C9CED6"),
	Text:         ui.Hex("#15181D"),
	Muted:        ui.Hex("#4A5563"),
	Subtle:       ui.Hex("#5B6573"),

	Accent:      ui.Hex("#0B6B83"),
	AccentHover: ui.Hex("#08586C"),
	AccentSoft:  ui.Hex("#E3F1F4"),
	AccentText:  ui.Hex("#FFFFFF"),

	Pending:      ui.Hex("#4353B8"),
	PendingSoft:  ui.Hex("#ECEEFB"),
	Conflict:     ui.Hex("#94480A"),
	ConflictSoft: ui.Hex("#FDF1E1"),
	Blocked:      ui.Hex("#B42318"),
	BlockedSoft:  ui.Hex("#FDECEA"),
	Ok:           ui.Hex("#136B41"),
	OkSoft:       ui.Hex("#E6F4EC"),
	Unknown:      ui.Hex("#525A66"),
	UnknownSoft:  ui.Hex("#EEF0F3"),
}

// Type scale (DIPs). Line heights are set where text wraps.
const (
	fsTitle    = 26 // page title
	fsSection  = 16 // card and section titles
	fsBody     = 13 // body, table cells, controls
	fsMeta     = 12 // secondary lines, captions
	fsOverline = 11 // group labels, uppercase
	fsMono     = 12 // IDs, paths, commands, digests
	fsStat     = 26 // attention counts
)

// Spacing scale (4-DIP grid).
const (
	sp1 = 4
	sp2 = 8
	sp3 = 12
	sp4 = 16
	sp5 = 20
	sp6 = 24
	sp8 = 32
)

const (
	radiusCard    = 8
	radiusControl = 6
	radiusPill    = 999

	windowW  = 1280
	windowH  = 800
	sidebarW = 236

	// Compact window: the minimum supported size. The sidebar becomes a
	// 56 DIP rail and pinned action bars keep critical controls visible.
	compactW = 1024
	compactH = 680
	railW    = 56
)

// theme derives the MyGo theme from the tokens so that built-in widgets
// (buttons, sidebar, table, segmented control, inputs) match custom ones.
func theme() *ui.Theme {
	t := ui.LightTheme().WithAccent(tk.Accent)
	t.Background = tk.Surface
	t.Surface = tk.Sunken
	t.SurfaceHover = ui.Hex("#EEF0F3")
	t.SurfacePressed = ui.Hex("#E4E7EB")
	t.Border = tk.Border
	t.Text = tk.Text
	t.TextMuted = tk.Muted
	t.AccentHover = tk.AccentHover
	t.Danger = tk.Blocked
	t.Warning = tk.Conflict
	t.Success = tk.Ok
	t.InactiveAccent = tk.Accent
	t.Focus = ui.Hex("#1A8FB0")
	t.FocusRingWidth = 2
	t.FocusRingOffset = 2
	t.Radius = radiusControl
	t.Spacing = 4
	t.FontSize = fsBody
	t.Font = fontUI
	return t
}

type tone int

const (
	toneNeutral tone = iota
	toneAccent
	tonePending
	toneConflict
	toneBlocked
	toneOk
	toneUnknown
	toneAttention // amber like conflict, but an alert: stale, unavailable, changed, outcome unknown
)

func (t tone) colors() (strong, soft ui.Color) {
	switch t {
	case toneAccent:
		return tk.Accent, tk.AccentSoft
	case tonePending:
		return tk.Pending, tk.PendingSoft
	case toneConflict:
		return tk.Conflict, tk.ConflictSoft
	case toneBlocked:
		return tk.Blocked, tk.BlockedSoft
	case toneOk:
		return tk.Ok, tk.OkSoft
	case toneUnknown:
		return tk.Unknown, tk.UnknownSoft
	case toneAttention:
		return tk.Conflict, tk.ConflictSoft
	}
	return tk.Muted, tk.Sunken
}

func (t tone) icon() *ui.SVG {
	switch t {
	case tonePending:
		return icClock
	case toneConflict:
		return icMerge
	case toneBlocked:
		return icBan
	case toneOk:
		return icCheckCircle
	case toneUnknown:
		return icDashed
	case toneAccent:
		return icInfo
	case toneAttention:
		return icAlert
	}
	return icInfo
}
