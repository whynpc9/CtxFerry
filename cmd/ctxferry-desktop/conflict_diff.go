package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/whynpc9/CtxFerry/internal/app"
)

type conflictTextDiff struct {
	base, version string
	parts         []diffmatchpatch.Diff
	open          bool
	viewer        *editor.DiffState
}

// Compute short inline differences before updating the UI. Longer texts use
// MyGo's virtualized diff viewer on demand to keep the three cards compact.
func conflictTextDiffs(detail app.ConflictDetail) [3]map[string]*conflictTextDiff {
	var diffs [3]map[string]*conflictTextDiff
	baseline := versionFields(detail.Base)
	match := diffmatchpatch.New()
	match.DiffTimeout = 25 * time.Millisecond
	for i, version := range [3]*app.ConflictVersion{nil, detail.Local, detail.Remote} {
		diffs[i] = make(map[string]*conflictTextDiff)
		for key, value := range versionFields(version) {
			if key == "record.deleted" {
				continue
			}
			before, haveBase := baseline[key].(string)
			after, haveVersion := value.(string)
			if !haveBase || !haveVersion || before == after {
				continue
			}
			diff := &conflictTextDiff{base: before, version: after}
			if len(before) <= 1200 && len(after) <= 1200 {
				parts := match.DiffCleanupSemantic(match.DiffMain(before, after, false))
				if len(parts) <= 30 {
					diff.parts = parts
				}
			}
			diffs[i][key] = diff
		}
	}
	return diffs
}

func (diff *conflictTextDiff) show(c *ui.Context, label string) {
	if diff.parts == nil {
		ui.Text(c, label+" · changed").FontSize(13).Bold().TextColor(tk.Muted)
		value := diff.version
		ui.TextArea(c, &value).ReadOnly(true).Label(label).Height(112).FillWidth()
		ui.Collapsible(c, "View line additions and removals", &diff.open, func() {
			if diff.viewer == nil {
				diff.viewer = &editor.DiffState{
					Old: editor.NewBuffer(diff.base), New: editor.NewBuffer(diff.version),
					Layout: editor.DiffUnified, Wrap: true, Indicators: editor.DiffClassic,
				}
			}
			editor.DiffView(c, diff.viewer).Height(180).FillWidth()
		})
		return
	}

	var spans []ui.Span
	var removed []string
	added := false
	for _, part := range diff.parts {
		span := ui.Span{Text: part.Text}
		switch part.Type {
		case diffmatchpatch.DiffDelete:
			removed = append(removed, part.Text)
			continue
		case diffmatchpatch.DiffInsert:
			span.Background = ui.Hex("#c5e6c1")
			added = true
		}
		spans = append(spans, span)
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, label).FontSize(13).Bold().TextColor(tk.Muted)
		if added {
			ui.Text(c, "+ Added").FontSize(13).Bold().TextColor(ui.Hex("#256042"))
		}
	})
	if diff.version == "" {
		ui.Text(c, "(empty text)").TextColor(tk.Muted)
	} else {
		ui.RichText(c, spans...).FontSize(15).TextColor(ink).Selectable()
	}
	if len(removed) > 0 {
		ui.Text(c, "− Removed").FontSize(13).Bold().TextColor(ui.Hex("#823632"))
		for _, text := range removed {
			ui.Text(c, text).TextColor(ui.Hex("#823632")).Strikethrough().Selectable()
		}
	}
}
