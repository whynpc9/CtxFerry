package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// ---------- Text roles ----------

func title(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(fsTitle).FontWeight(680).LetterSpacing(-0.5)
}

func h2(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(fsSection).FontWeight(650).LetterSpacing(-0.15)
}

func body(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(fsBody).LineHeight(1.45)
}

func strong(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(fsBody).FontWeight(600)
}

func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(fsMeta).TextColor(tk.Muted).LineHeight(1.4)
}

func subtle(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(fsMeta).TextColor(tk.Subtle)
}

func overline(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(fsOverline).FontWeight(600).LetterSpacing(0.6).TextColor(tk.Subtle)
}

func mono(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).Font(fontMono).FontSize(fsMono)
}

func monoMuted(c *ui.Context, s string) ui.Element {
	return mono(c, s).TextColor(tk.Muted)
}

func ico(c *ui.Context, s *ui.SVG, size float32, col ui.Color) ui.Element {
	return ui.Icon(c, s).FontSize(size).TextColor(col)
}

// ---------- Containers ----------

func card(c *ui.Context) ui.Element {
	return ui.Column(c).Background(tk.Surface).Border(1, tk.Border).Radius(radiusCard).Padding(sp4).Gap(sp3)
}

func well(c *ui.Context) ui.Element {
	return ui.Column(c).Background(tk.Sunken).Radius(radiusControl).Padding(sp3).Gap(sp2)
}

// cardHeader: icon, title, optional subtitle, trailing content.
func cardHeader(c *ui.Context, icon *ui.SVG, t, sub string, trailing func()) {
	ui.Row(c).Gap(sp3).AlignItems(ui.Start).Children(func() {
		if icon != nil {
			ui.Box(c).Size(28, 28).Radius(radiusControl).Background(tk.AccentSoft).Center().Shrink(0).Children(func() {
				ico(c, icon, 15, tk.Accent)
			})
		}
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			h2(c, t)
			if sub != "" {
				muted(c, sub)
			}
		})
		if trailing != nil {
			ui.Row(c).Gap(sp2).Shrink(0).Children(trailing)
		}
	})
}

// ---------- Status ----------

// pill is a compact status label: icon + text on the tone's soft fill.
func pill(c *ui.Context, t tone, label string) ui.Element {
	strongC, soft := t.colors()
	return ui.Row(c).Gap(5).Padding(2, 8, 2, 6).Radius(radiusPill).Background(soft).Shrink(0).Children(func() {
		ico(c, t.icon(), 12, strongC)
		cut(ui.Text(c, label).FontSize(fsMeta).FontWeight(560).TextColor(strongC))
	})
}

// tag is a neutral label for kinds and layers.
func tag(c *ui.Context, label string) ui.Element {
	return ui.Row(c).Padding(1, 7).Radius(4).Background(tk.UnknownSoft).Shrink(0).Children(func() {
		cut(ui.Text(c, label).FontSize(fsMeta - 0.5).FontWeight(520).TextColor(tk.Muted))
	})
}

// idChip shows a shortened stable ID as quiet mono text; the full value is
// in its tooltip. No fill: IDs are evidence, not controls.
func idChip(c *ui.Context, id string) ui.Element {
	return ui.Row(c).Padding(1, 2).Shrink(0).Tooltip(id).Children(func() {
		ui.Text(c, shortID(id)).Font(fontMono).FontSize(fsMono - 1).TextColor(tk.Subtle)
	})
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func cut(e ui.Element) ui.Element { return e.SingleLine() }

func dot(c *ui.Context, col ui.Color) ui.Element {
	return ui.Box(c).Size(8, 8).Radius(4).Background(col).Shrink(0)
}

// ---------- Key/value ----------

type kvRow struct {
	k    string
	v    string
	mono bool
	fn   func() // custom value, overrides v
}

func kvGrid(c *ui.Context, rows []kvRow) ui.Element {
	return ui.Grid(c).ColumnTracks(ui.Fixed(136), ui.Fr(1)).GapX(sp3).GapY(sp2 + 2).Children(func() {
		for _, r := range rows {
			ui.Text(c, r.k).FontSize(fsMeta).TextColor(tk.Muted).Padding(1, 0, 0, 0)
			switch {
			case r.fn != nil:
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Wrap().Children(r.fn)
			case r.mono:
				mono(c, r.v)
			default:
				body(c, r.v)
			}
		}
	})
}

// ---------- Buttons ----------

func iconButton(c *ui.Context, icon *ui.SVG, label string, primary bool) ui.Element {
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c, "")
	} else {
		b = ui.Button(c, "")
	}
	return b.Children(func() {
		ui.Icon(c, icon).FontSize(14)
		cut(ui.Text(c, label))
	})
}

func linkButton(c *ui.Context, label string, icon *ui.SVG) ui.Element {
	return ui.ButtonBase(c).Gap(4).Padding(2, 0).Cursor(ui.CursorPointer).Children(func() {
		cut(ui.Text(c, label).FontSize(fsBody).FontWeight(560).TextColor(tk.Accent))
		if icon != nil {
			ico(c, icon, 13, tk.Accent)
		}
	})
}

// ---------- Command and CLI handoff ----------

// cmdBlock is a command the operator runs elsewhere. Its copy action is a
// labelled native button above the command, never a bare icon, and it is the
// primary action of its card unless the caller says otherwise.
func cmdBlock(c *ui.Context, cmd string) ui.Element {
	return cmdBlockP(c, cmd, true)
}

func cmdBlockP(c *ui.Context, cmd string, primary bool) ui.Element {
	first := strings.SplitN(cmd, " \\", 2)[0]
	return ui.Column(c).Gap(sp2).Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			ico(c, icTerminal, 13, tk.Muted)
			muted(c, "Run in a terminal").Grow(1)
			iconButton(c, icCopy, "Copy command", primary).Label("Copy command: " + first).Tooltip("Copies the full command, including --data-dir")
		})
		ui.Row(c).Background(ui.Hex("#11161C")).Radius(radiusControl).Padding(sp2+2, sp3).Gap(sp3).AlignItems(ui.Start).Children(func() {
			ui.Text(c, "$").Font(fontMono).FontSize(fsMono).TextColor(ui.Hex("#94A0AE")).Shrink(0)
			ui.Text(c, cmd).Font(fontMono).FontSize(fsMono).TextColor(ui.Hex("#E6EDF3")).LineHeight(1.5).Grow(1)
		})
	})
}

// quietButton is a borderless accent action for a step that comes after the
// main one, such as Refresh once a CLI command has finished.
func quietButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	return ui.ButtonBase(c).Gap(5).Padding(4, 6).Radius(radiusControl).Cursor(ui.CursorPointer).Shrink(0).Label(label).Children(func() {
		ico(c, icon, 13, tk.Accent)
		cut(ui.Text(c, label).FontSize(fsBody).FontWeight(560).TextColor(tk.Accent))
	})
}

// afterRun is the return step of every CLI handoff: what Refresh will read,
// and a quiet Refresh. It stays subordinate to Copy command until the
// operator has run the command.
func afterRun(c *ui.Context, after, label string) ui.Element {
	if label == "" {
		label = "Refresh"
	}
	return ui.Row(c).Gap(sp2).Padding(sp2, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).AlignItems(ui.Center).Children(func() {
		muted(c, after).Grow(1).Shrink(1)
		quietButton(c, icSync, label)
	})
}

type prereq struct {
	t     tone
	label string
}

type handoff struct {
	title    string
	why      string
	prereqs  []prereq
	commands []string
	after    string
	refresh  string // label of the quiet Refresh; "Refresh" if empty
}

func cli(dataRoot, first string, more ...string) string {
	lines := append([]string{"ctxferry " + first}, more...)
	lines = append(lines, "--data-dir "+dataRoot)
	return strings.Join(lines, " \\\n    ")
}

// cliHandoff is the standard card for an action that continues in the CLI:
// prerequisites, the exact command (which carries the data root), and how
// to come back.
func cliHandoff(c *ui.Context, h handoff) ui.Element {
	return card(c).Children(func() {
		cardHeader(c, icTerminal, h.title, h.why, nil)
		if len(h.prereqs) > 0 {
			ui.Column(c).Gap(6).Children(func() {
				for _, p := range h.prereqs {
					ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
						col, _ := p.t.colors()
						ico(c, p.t.icon(), 14, col)
						body(c, p.label)
					})
				}
			})
		}
		for i, cmd := range h.commands {
			cmdBlockP(c, cmd, i == 0)
		}
		afterRun(c, h.after, h.refresh)
	})
}

// ---------- Preview and receipt ----------

type effect struct {
	t      tone
	target string
	detail string
}

// confirmSpec is the single confirmation contract used by every preview in
// every window size: the token the confirm is bound to, the button that
// names the exact target, and the way back. Full-width panels render it as
// a footer; compact windows render the same strings in the pinned bar.
type confirmSpec struct {
	tokenLabel string // "preview token" or "conflict token"
	token      string
	cancel     string
	confirm    string
	icon       *ui.SVG // optional icon on the primary button
}

// confirmToken is the complete label: what the confirm checks, and its ID.
func confirmToken(c *ui.Context, cf confirmSpec) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
		ico(c, icKey, 13, tk.Muted)
		cut(ui.Text(c, "Confirms against "+cf.tokenLabel).FontSize(fsMeta).TextColor(tk.Muted))
		idChip(c, cf.token)
	})
}

func confirmButtons(c *ui.Context, cf confirmSpec) {
	ui.Button(c, cf.cancel)
	if cf.icon != nil {
		iconButton(c, cf.icon, cf.confirm, true)
	} else {
		ui.PrimaryButton(c, cf.confirm)
	}
}

// confirmFooter closes a preview panel: the token line directly above the
// buttons it governs, so neither is ever truncated by the other.
func confirmFooter(c *ui.Context, cf confirmSpec) {
	ui.Column(c).Gap(sp2).Padding(sp3, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).Children(func() {
		confirmToken(c, cf)
		ui.Row(c).Gap(sp2).Justify(ui.End).Children(func() { confirmButtons(c, cf) })
	})
}

// confirmBar is the compact rendering of the same contract, in one row of
// the action bar pinned under the page.
func confirmBar(c *ui.Context, cf confirmSpec) {
	confirmToken(c, cf)
	ui.Spacer(c)
	confirmButtons(c, cf)
}

// effectRows lists what a preview will change, one target per row.
func effectRows(c *ui.Context, effects []effect) {
	ui.Column(c).Gap(0).BorderWidth(1, 0, 1, 0).BorderColor(tk.Border).Dividers(1, tk.Border).Children(func() {
		for _, e := range effects {
			ui.Row(c).Gap(sp3).Padding(sp2, 0).AlignItems(ui.Center).Children(func() {
				col, _ := e.t.colors()
				ico(c, e.t.icon(), 15, col)
				ui.Text(c, e.target).FontSize(fsBody).FontWeight(560).Width(196).Shrink(0)
				body(c, e.detail).TextColor(tk.Muted).Grow(1)
			})
		}
	})
}

// previewBadge marks unconfirmed content. Dashed border: nothing has
// happened yet.
func previewBadge(c *ui.Context) {
	ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Padding(2, 8).Radius(4).Background(tk.Text).Children(func() {
			ui.Text(c, "PREVIEW").FontSize(fsOverline).FontWeight(650).LetterSpacing(0.6).TextColor(tk.Surface)
		})
		muted(c, "Nothing has changed yet")
	})
}

// previewPanel describes a possible effect. With cf == nil the confirm is
// pinned in the shell's action bar instead (compact windows).
func previewPanel(c *ui.Context, heading, scope string, effects []effect, cf *confirmSpec, extra func()) ui.Element {
	return ui.Column(c).Gap(sp3).Padding(sp4).Radius(radiusCard).Background(tk.Surface).
		Border(1.5, tk.BorderStrong).BorderStyle(ui.BorderDashed).Children(func() {
		previewBadge(c)
		h2(c, heading)
		if scope != "" {
			muted(c, scope)
		}
		effectRows(c, effects)
		if extra != nil {
			extra()
		}
		if cf != nil {
			confirmFooter(c, *cf)
		}
	})
}

// previewStrip is the compact rendering of a preview: badge and heading on
// one line, the same effects laid out across columns. Its confirm is the
// pinned confirmBar.
func previewStrip(c *ui.Context, heading string, effects []effect) ui.Element {
	return ui.Column(c).Gap(sp3).Padding(sp3, sp4).Radius(radiusCard).Background(tk.Surface).
		Border(1.5, tk.BorderStrong).BorderStyle(ui.BorderDashed).Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Padding(2, 8).Radius(4).Background(tk.Text).Children(func() {
				ui.Text(c, "PREVIEW").FontSize(fsOverline).FontWeight(650).LetterSpacing(0.6).TextColor(tk.Surface)
			})
			h2(c, heading)
			muted(c, "· Nothing has changed yet")
		})
		tracks := make([]ui.Track, len(effects))
		for i := range tracks {
			tracks[i] = ui.Fr(1)
		}
		ui.Grid(c).ColumnTracks(tracks...).GapX(sp4).Children(func() {
			for _, e := range effects {
				ui.Row(c).Gap(sp2).AlignItems(ui.Start).Children(func() {
					col, _ := e.t.colors()
					ico(c, e.t.icon(), 15, col).Margin(2, 0, 0, 0)
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						strong(c, e.target)
						muted(c, e.detail)
					})
				})
			}
		})
	})
}

// listFooter closes every list: the truthful displayed range and the way
// to see more.
func listFooter(c *ui.Context, text string, actions func()) ui.Element {
	return ui.Row(c).Gap(sp2).Padding(sp2, sp3).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).AlignItems(ui.Center).Children(func() {
		muted(c, text).Grow(1)
		if actions != nil {
			actions()
		}
	})
}

// evidence keeps the three facts a change can have apart, in one fixed
// order and vocabulary: what was written locally, what a reread found, and
// what a confirmed commit has published. Only the last means other devices
// can see it.
// evidence states what was written, what was read and what was published.
// READ BACK is used only when the destination itself was read again after
// the write. A write taken from an earlier read (a backup) names that read
// as its SOURCE SNAPSHOT instead, before the write.
type evidence struct {
	written, writtenDetail string
	read, readDetail       string
	source, sourceDetail   string
	published, pubDetail   string
	pubTone                tone
}

func evidenceStrip(c *ui.Context, e evidence) ui.Element {
	return ui.Row(c).Radius(radiusControl).Border(1, tk.Border).Dividers(1, tk.Border).AlignItems(ui.Stretch).Children(func() {
		cell := func(icon *ui.SVG, label string, t tone, value, detail string) {
			ui.Column(c).Grow(1).Basis(0).Gap(4).Padding(sp2+2, sp3).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ico(c, icon, 12, tk.Muted)
					overline(c, label)
				})
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					col, _ := t.colors()
					vi := t.icon()
					if label == "SOURCE SNAPSHOT" {
						vi, col = icCommit, tk.Text
					}
					ico(c, vi, 13, col)
					ui.Text(c, value).FontSize(fsBody).FontWeight(600).TextColor(col)
				})
				if detail != "" {
					muted(c, detail)
				}
			})
		}
		if e.source != "" {
			cell(icDatabase, "SOURCE SNAPSHOT", toneNeutral, e.source, e.sourceDetail)
			cell(icDrive, "WRITTEN", toneOk, e.written, e.writtenDetail)
		} else {
			cell(icDrive, "WRITTEN", toneOk, e.written, e.writtenDetail)
			cell(icEye, "READ BACK", toneOk, e.read, e.readDetail)
		}
		cell(icUpload, "PUBLISHED", e.pubTone, e.published, e.pubDetail)
	})
}

// evidenceList is the same three facts as evidenceStrip, stacked as rows
// for narrow detail columns.
func evidenceList(c *ui.Context, e evidence) ui.Element {
	return ui.Column(c).Radius(radiusControl).Border(1, tk.Border).Dividers(1, tk.Border).Children(func() {
		row := func(icon *ui.SVG, label string, t tone, value, detail string) {
			ui.Row(c).Gap(sp3).Padding(sp2, sp3).AlignItems(ui.Start).Children(func() {
				ui.Row(c).Gap(6).Width(128).Shrink(0).AlignItems(ui.Center).Padding(2, 0, 0, 0).Children(func() {
					ico(c, icon, 12, tk.Muted)
					overline(c, label)
				})
				ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						col, _ := t.colors()
						ico(c, t.icon(), 13, col)
						ui.Text(c, value).FontSize(fsBody).FontWeight(600).TextColor(col)
					})
					if detail != "" {
						muted(c, detail)
					}
				})
			})
		}
		row(icDrive, "WRITTEN", toneOk, e.written, e.writtenDetail)
		row(icEye, "READ BACK", toneOk, e.read, e.readDetail)
		row(icUpload, "PUBLISHED", e.pubTone, e.published, e.pubDetail)
	})
}

type receipt struct {
	heading string
	rows    []kvRow
	ev      evidence
	actions func()
}

// receiptPanel records a completed operation: target and scope, then the
// written / read back / published evidence, then the way on.
func receiptPanel(c *ui.Context, r receipt) ui.Element {
	return ui.Row(c).Radius(radiusCard).Border(1, tk.Border).Background(tk.Surface).Clip().Children(func() {
		ui.Box(c).Width(4).FillHeight().Background(tk.Ok)
		ui.Column(c).Grow(1).Padding(sp4).Gap(sp3).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icCheckCircle, 16, tk.Ok)
				ui.Text(c, "RECEIPT").FontSize(fsOverline).FontWeight(650).LetterSpacing(0.6).TextColor(tk.Ok)
			})
			h2(c, r.heading)
			kvGrid(c, r.rows)
			evidenceStrip(c, r.ev)
			if r.actions != nil {
				ui.Row(c).Gap(sp2).Children(r.actions)
			}
		})
	})
}

// ---------- Banners ----------

func banner(c *ui.Context, t tone, heading, text string, actions func()) ui.Element {
	strongC, soft := t.colors()
	return ui.Row(c).Gap(sp3).Padding(sp3, sp4).Radius(radiusCard).Background(soft).Border(1, strongC.Alpha(0.25)).AlignItems(ui.Center).Children(func() {
		ico(c, t.icon(), 18, strongC).AlignSelf(ui.Start).Margin(1, 0, 0, 0)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, heading).FontSize(fsBody).FontWeight(620).TextColor(strongC)
			if text != "" {
				body(c, text).TextColor(tk.Text)
			}
		})
		if actions != nil {
			ui.Row(c).Gap(sp2).Shrink(0).Children(actions)
		}
	})
}

// ---------- Scope bar ----------

type scopeItem struct {
	label string
	value string
	id    string
	icon  *ui.SVG
}

// scopeBar states exactly which Project, Checkout, Profile and other IDs
// the page is scoped to. It is an unboxed context line under the title:
// sentence-case labels, values, quiet IDs and hairline separators.
func scopeBar(c *ui.Context, items []scopeItem, trailing func()) ui.Element {
	return ui.Row(c).Gap(sp4).Padding(0, 0, sp3, 0).BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).AlignItems(ui.Center).Children(func() {
		for i, it := range items {
			if i > 0 {
				ui.Box(c).Width(1).Height(18).Background(tk.Border)
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, scopeLabel(it.label)).FontSize(fsMeta).TextColor(tk.Muted)
				if it.icon != nil {
					ico(c, it.icon, 13, tk.Muted)
				}
				ui.Text(c, it.value).FontSize(fsBody).FontWeight(600)
				if it.id != "" {
					idChip(c, it.id)
				}
			})
		}
		ui.Spacer(c)
		if trailing != nil {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(trailing)
		}
	})
}

// scopeLabel turns "FROM TASK · SLOT" into "From Task · slot".
func scopeLabel(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		switch w {
		case "task", "project", "checkout", "profile", "resource", "skill":
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	if len(words) > 0 {
		words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	}
	return strings.Join(words, " ")
}

// ---------- Page header ----------

func pageHeader(c *ui.Context, t, sub string, actions func()) {
	ui.Row(c).Gap(sp4).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			title(c, t)
			if sub != "" {
				ui.Text(c, sub).FontSize(14).LineHeight(1.45).TextColor(tk.Muted)
			}
		})
		if actions != nil {
			ui.Row(c).Gap(sp2).Shrink(0).Children(actions)
		}
	})
}

// sectionLabel is a small heading above a group inside a page.
func sectionLabel(c *ui.Context, s string, trailing func()) {
	ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
		overline(c, s).Grow(1)
		if trailing != nil {
			trailing()
		}
	})
}

// listRow is a selectable row for master lists. Selection uses the accent
// soft fill plus a 3-DIP accent bar so it never depends on colour alone.
func listRow(c *ui.Context, selected bool, fn func()) ui.Element {
	r := ui.Row(c).Gap(sp3).Padding(sp2+2, sp3).AlignItems(ui.Center).Radius(radiusControl)
	if selected {
		r.Background(tk.AccentSoft).AccessSelected(true)
		r.Children(func() {
			ui.Box(c).Absolute().Left(0).Top(7).Bottom(7).Width(3).Radius(2).Background(tk.Accent)
			fn()
		})
		return r
	}
	return r.Children(fn)
}

func stepNumber(c *ui.Context, n string, t tone) ui.Element {
	strongC, soft := t.colors()
	return ui.Box(c).Size(24, 24).Radius(12).Background(soft).Center().Shrink(0).Children(func() {
		if t == toneOk {
			ico(c, icCheck, 13, strongC)
		} else {
			ui.Text(c, n).FontSize(fsMeta).FontWeight(650).TextColor(strongC)
		}
	})
}
