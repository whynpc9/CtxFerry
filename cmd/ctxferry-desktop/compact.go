package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func (d *desktop) compactActionBar(c *ui.Context) {
	var build func()
	switch {
	case d.route.Page == pageProjects && d.project.editor.open && d.project.editor.preview != nil:
		p := &d.project
		preview := *p.editor.preview
		build = func() {
			confirmToken(c, confirmSpec{tokenLabel: "preview token", token: preview.PreviewID})
			ui.Spacer(c)
			ui.Button(c, "Cancel").OnClick(func() { p.editor.open = false })
			label := "Save to " + layerLabel(p.layer)
			if p.editor.reset {
				label = "Remove from " + layerLabel(p.layer)
			}
			ui.PrimaryButton(c, label).Disabled(p.busy).OnClick(p.applyChange)
		}
	case d.route.Page == pageConflict && d.conflict != nil && d.conflict.choice != "" && d.conflict.revision == "":
		p := d.conflict
		build = func() {
			confirmToken(c, confirmSpec{tokenLabel: "conflict token", token: p.detail.ConflictID})
			ui.Spacer(c)
			ui.Button(c, "Cancel").Disabled(p.writing).OnClick(d.back)
			ui.PrimaryButton(c, choiceLabel(p.choice, p.version(p.choice))).Label("Confirm " + choiceLabel(p.choice, p.version(p.choice))).Disabled(!p.canConfirm()).OnClick(func() { d.resolveConflict(p) })
		}
	case d.route.Page == pageSync && d.syncState.tab == syncBackupTab && d.syncState.backupReview && d.syncState.backupAt.IsZero():
		s := &d.syncState
		build = func() {
			ui.Checkbox(c, &s.backupAck, "I reviewed the plaintext exposure").Label("Acknowledge plaintext backup exposure")
			ui.Spacer(c)
			ui.Button(c, "Cancel").OnClick(func() { s.backupReview, s.backupAck = false, false })
			ui.PrimaryButton(c, "Write backup").Disabled(!s.backupAck).OnClick(func() { d.createBackup(strings.TrimSpace(s.backupPath), s.includeLocal) })
		}
	}
	if build == nil {
		return
	}
	ui.Row(c).Shrink(0).Gap(sp2).Padding(sp3, sp4).AlignItems(ui.Center).Background(tk.Sunken).
		BorderWidth(1, 0, 0, 0).BorderColor(tk.BorderStrong).Label("Confirmation actions").Children(build)
}
