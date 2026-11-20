package main

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

// WCAG 2.x relative luminance and contrast ratio for opaque sRGB colours.
func luminance(c ui.Color) float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

func contrast(a, b ui.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

type contrastPair struct {
	use      string
	fg, bg   ui.Color
	fgN, bgN string
}

// contrastPairs lists every text colour on every surface it is drawn on in
// the frames. Small text needs 4.5:1, so every pair is held to 4.5.
func contrastPairs() []contrastPair {
	p := func(use string, fgN string, fg ui.Color, bgN string, bg ui.Color) contrastPair {
		return contrastPair{use, fg, bg, fgN, bgN}
	}
	var out []contrastPair
	surfaces := []struct {
		n string
		c ui.Color
	}{{"Page", tk.Page}, {"Surface", tk.Surface}, {"Sunken", tk.Sunken}, {"Canvas", tk.Canvas}, {"AccentSoft (selected row)", tk.AccentSoft}}
	for _, s := range surfaces {
		out = append(out,
			p("body text", "Text", tk.Text, s.n, s.c),
			p("muted evidence, kv keys, IDs", "Muted", tk.Muted, s.n, s.c),
			p("subtle meta, overlines", "Subtle", tk.Subtle, s.n, s.c),
		)
	}
	out = append(out,
		p("links, accent text", "Accent", tk.Accent, "Surface", tk.Surface),
		p("links in sunken wells", "Accent", tk.Accent, "Sunken", tk.Sunken),
		p("links on the page", "Accent", tk.Accent, "Page", tk.Page),
		p("evidence: not yet published", "Pending", tk.Pending, "Surface", tk.Surface),
		p("summary line: conflict count", "Conflict", tk.Conflict, "Page", tk.Page),
		p("summary line: blocked count", "Blocked", tk.Blocked, "Page", tk.Page),
		p("summary line: waiting count", "Pending", tk.Pending, "Page", tk.Page),
		p("primary button label", "AccentText", tk.AccentText, "Accent", tk.Accent),
		p("pill: info", "Accent", tk.Accent, "AccentSoft", tk.AccentSoft),
		p("pill: waiting", "Pending", tk.Pending, "PendingSoft", tk.PendingSoft),
		p("pill: conflict, attention", "Conflict", tk.Conflict, "ConflictSoft", tk.ConflictSoft),
		p("pill: blocked", "Blocked", tk.Blocked, "BlockedSoft", tk.BlockedSoft),
		p("pill: ok", "Ok", tk.Ok, "OkSoft", tk.OkSoft),
		p("pill: unknown", "Unknown", tk.Unknown, "UnknownSoft", tk.UnknownSoft),
		p("tag (Profile, record kind)", "Muted", tk.Muted, "UnknownSoft", tk.UnknownSoft),
		p("pill: neutral", "Muted", tk.Muted, "Sunken", tk.Sunken),
		p("ok text on white", "Ok", tk.Ok, "Surface", tk.Surface),
		p("conflict text on white", "Conflict", tk.Conflict, "Surface", tk.Surface),
		p("blocked text on white", "Blocked", tk.Blocked, "Surface", tk.Surface),
		p("body text in tone banners", "Text", tk.Text, "ConflictSoft", tk.ConflictSoft),
		p("muted text in tone wells", "Muted", tk.Muted, "ConflictSoft", tk.ConflictSoft),
		p("exposure group overlines", "Subtle", tk.Subtle, "ConflictSoft", tk.ConflictSoft),
		p("muted text in ok wells", "Muted", tk.Muted, "OkSoft", tk.OkSoft),
		p("muted text in blocked wells", "Muted", tk.Muted, "BlockedSoft", tk.BlockedSoft),
		p("muted text in waiting wells", "Muted", tk.Muted, "PendingSoft", tk.PendingSoft),
		p("body text in blocked wells", "Text", tk.Text, "BlockedSoft", tk.BlockedSoft),
		p("rail badge count", "Surface", tk.Surface, "Conflict", tk.Conflict),
		p("action bar text", "Muted", tk.Muted, "Sunken", tk.Sunken),
		p("command text", "#E6EDF3", ui.Hex("#E6EDF3"), "#11161C", ui.Hex("#11161C")),
		p("command prompt", "#94A0AE", ui.Hex("#94A0AE"), "#11161C", ui.Hex("#11161C")),
	)
	return out
}

func runContrast() int {
	bad := 0
	for _, x := range contrastPairs() {
		r := contrast(x.fg, x.bg)
		mark := "ok"
		if r < 4.5 {
			mark = "FAIL"
			bad++
		}
		fmt.Printf("%-4s %5.2f  %-28s on %-26s %s\n", mark, r, x.fgN, x.bgN, x.use)
	}
	return bad
}
