package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

var conflictBorder = tk.Border

const conflictReceipt = "Local revision created"

func init() {
	desktopPages[pageConflict] = (*desktop).conflictView
}

func identifier(c *ui.Context, label, value string) {
	ui.Text(c, label+" "+value).Font(fontMono).FontSize(fsMono).FillWidth().TextColor(tk.Muted).Selectable()
}

func (d *desktop) conflictToolbar(c *ui.Context) {
	p := d.conflict
	ui.Button(c, "Back to Queue").Disabled(p.writing).OnClick(d.back)
	if p.revision == "" {
		ui.Button(c, "Refresh comparison").Disabled(p.loading || p.writing).OnClick(d.loadConflict)
	}
}

func (d *desktop) conflictView(c *ui.Context) {
	p := d.conflict
	if p == nil {
		return
	}
	if p.revision != "" {
		read := "Reread pending"
		if !d.data.ReadAt.IsZero() && !d.data.ReadAt.Before(p.resolvedAt) {
			read = "Reread " + d.data.ReadAt.Local().Format("15:04:05")
		}
		receiptPanel(c, receipt{
			heading: conflictReceipt,
			rows: []kvRow{
				{k: "Document", v: p.resolvedTitle()},
				{k: "Resolution", v: choiceLabel(p.choice, p.version(p.choice)) + " · confirmed against conflict token"},
				{k: "Record ID", v: p.id, mono: true},
				{k: "Local revision", v: p.revision, mono: true},
			},
			ev: evidence{
				written: "Saved " + p.resolvedAt.Local().Format("15:04:05"), writtenDetail: "Local revision " + p.revision,
				read: read, readDetail: "The Queue was refreshed after the write",
				published: "Not yet", pubDetail: valueOr(d.waitingHold(), "Queued for the running service"), pubTone: tonePending,
			},
			actions: func() {
				ui.PrimaryButton(c, "View waiting revision").Disabled(d.loading).OnClick(func() {
					d.back()
					d.syncState.setFilter(queuePending)
				})
			},
		})
		return
	}
	pageHeader(c, "Resolve conflict", "Keep one whole side, or replace the document with new content. The result is saved locally, then queued for publication.", func() {
		ui.Button(c, "Refresh comparison").Disabled(p.loading || p.writing).OnClick(d.loadConflict)
	})
	if p.detail.ConflictID != "" {
		items := []scopeItem{{label: "Record", value: p.title(), id: p.id, icon: icFile}, {label: "Conflict token", value: shortID(p.detail.ConflictID), id: p.detail.ConflictID, icon: icKey}}
		if p.detail.Project != "" {
			items = append([]scopeItem{{label: "Project", value: p.detail.Project, icon: icFolder}}, items...)
		}
		scopeBar(c, items, nil)
	}
	if p.loading {
		muted(c, "Reading base, local and remote versions…")
	}
	if p.err != "" {
		banner(c, toneAttention, "Comparison needs attention", p.err, nil)
	}
	if p.detail.ConflictID == "" {
		return
	}
	d.conflictComparison(c, p)
	d.conflictResolution(c, p)
}

func (p *conflictPage) title() string {
	for _, v := range []*app.ConflictVersion{p.detail.Local, p.detail.Remote, p.detail.Base} {
		if name := conflictVersionTitle(v); name != "" {
			return name
		}
	}
	return p.id
}

func (p *conflictPage) resolvedTitle() string {
	if p.choice == "edit" {
		return p.draft.input.Title
	}
	if title := conflictVersionTitle(p.version(p.choice)); title != "" {
		return title
	}
	return p.title()
}

func conflictVersionTitle(v *app.ConflictVersion) string {
	if v != nil {
		for _, field := range []string{"title", "name", "slug"} {
			if label, ok := payload(v)[field].(string); ok && label != "" {
				return label
			}
		}
	}
	return ""
}

func (p *conflictPage) description() string {
	kind := "Shared record"
	if v := p.detail.Local; v != nil {
		kind = v.Kind
		if kind == "context" {
			kind = "Context document"
		}
	}
	parts := []string{p.title(), kind}
	if p.detail.Project != "" {
		parts = append([]string{p.detail.Project}, parts...)
	}
	return strings.Join(parts, "  /  ")
}

type comparisonField struct {
	key     string
	label   string
	changed bool
}

func versionFields(v *app.ConflictVersion) map[string]any {
	if v == nil {
		return nil
	}
	fields := map[string]any{
		"policy.storage": v.Policy.Storage, "policy.agentDisclosure": v.Policy.AgentDisclosure,
		"record.deleted": "Active",
	}
	if v.Deleted {
		fields["record.deleted"] = "Deleted"
	}
	for key, value := range payload(v) {
		fields[key] = value
	}
	return fields
}

func comparisonFields(versions [3]map[string]any) []comparisonField {
	keys := make(map[string]bool)
	for _, fields := range versions {
		for key := range fields {
			keys[key] = true
		}
	}
	labels := map[string]string{
		"body": "Body", "summary": "Document summary", "title": "Title", "purpose": "Purpose",
		"source": "Source", "observedAt": "Observed at", "validUntil": "Valid until",
		"policy.agentDisclosure": "Agent disclosure", "policy.storage": "Storage scope", "record.deleted": "Record state",
	}
	order := []string{"body", "summary", "title", "purpose", "source", "policy.agentDisclosure", "policy.storage"}
	var remaining []string
	for key := range keys {
		if !slices.Contains(order, key) {
			remaining = append(remaining, key)
		}
	}
	slices.Sort(remaining)
	var fields []comparisonField
	for _, key := range append(order, remaining...) {
		if !keys[key] {
			continue
		}
		label := labels[key]
		if label == "" {
			label = key
		}
		value, present := versions[1][key]
		changed := false
		for _, side := range []int{0, 2} {
			if versions[side] != nil {
				other, otherPresent := versions[side][key]
				changed = changed || otherPresent != present || !reflect.DeepEqual(other, value)
			}
		}
		fields = append(fields, comparisonField{key, label, changed})
	}
	return fields
}

func (d *desktop) conflictComparison(c *ui.Context, p *conflictPage) {
	versions := [3]*app.ConflictVersion{p.detail.Base, p.detail.Local, p.detail.Remote}
	names := [3]string{"Common base", "Local version", "Remote version"}
	choices := [3]string{"", "local", "remote"}
	maps := [3]map[string]any{versionFields(versions[0]), versionFields(versions[1]), versionFields(versions[2])}
	fields := comparisonFields(maps)
	changed := 0
	for _, field := range fields {
		if field.changed {
			changed++
		}
	}
	ui.Row(c).FillWidth().Gap(sp2).Wrap().Children(func() {
		section(c, "Three-way comparison")
		muted(c, fmt.Sprintf("%d visible fields differ", changed))
		ui.Spacer(c)
		ui.Checkbox(c, &p.all, "Show all fields")
	})
	width, _ := c.Size()
	narrow := width < 980
	cards := ui.Row(c).FillWidth().AlignItems(ui.Stretch).Gap(sp3)
	if narrow {
		cards.Column()
	}
	cards.Children(func() {
		for i, v := range versions {
			border := conflictBorder
			selected := p.choice != "" && p.choice == choices[i]
			if selected {
				border = tk.Accent
			}
			card := ui.Column(c).Key(names[i]).MinWidth(0).Gap(sp2).Padding(sp4).Border(1, border).Radius(radiusCard).Background(tk.Surface)
			if !narrow {
				card.Grow(1).Basis(0)
			}
			card.Children(func() {
				d.conflictCard(c, names[i], v, selected, maps[i], fields, p.diffs[i], p.all)
			})
		}
	})
}

func (d *desktop) conflictCard(c *ui.Context, name string, v *app.ConflictVersion, selected bool, values map[string]any, fields []comparisonField, diffs map[string]*conflictTextDiff, all bool) {
	ui.Row(c).FillWidth().Gap(sp2).Children(func() {
		h2(c, name)
		if selected {
			pill(c, toneAccent, "Selected")
		}
	})
	if v == nil {
		muted(c, "This side does not exist")
		return
	}
	strong(c, d.conflictDevice(v.Provenance.DeviceID))
	monoMuted(c, v.Provenance.DeviceID).Selectable()
	muted(c, conflictTime(v.Provenance.UpdatedAt))
	identifier(c, "Revision", v.Revision)
	if v.Deleted {
		pill(c, toneBlocked, "Deleted on this side")
	}
	ui.Divider(c)
	for _, field := range fields {
		if !all && !field.changed && !(field.key == "record.deleted" && v.Deleted) {
			continue
		}
		if diff := diffs[field.key]; diff != nil {
			diff.show(c, field.label)
			continue
		}
		value, present := values[field.key]
		label := field.label
		if field.changed {
			label += " · changed"
		}
		ui.Text(c, label).FontSize(fsMeta).Bold().TextColor(tk.Muted)
		conflictValue(c, v, field, value, present)
	}
	if v.Kind == "context" && v.Policy.AgentDisclosure != "full" {
		muted(c, "Body hidden by the disclosure policy")
	}
}

func (d *desktop) conflictDevice(id string) string {
	if id == "" {
		return "Unknown device"
	}
	if local := d.data.Device; id == local.ID && local.Name != "" {
		return local.Name + " · " + local.OS + "/" + local.Arch
	}
	return "Unknown device environment"
}

func conflictTime(raw string) string {
	stamp, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	stamp = stamp.Local()
	if stamp.Format("2006-01-02") == time.Now().Format("2006-01-02") {
		return "today " + stamp.Format("15:04:05")
	}
	return stamp.Format("2006-01-02 15:04:05")
}

func conflictValue(c *ui.Context, v *app.ConflictVersion, field comparisonField, value any, present bool) {
	switch {
	case v.Kind == "context" && !present && field.key == "body" && v.Policy.AgentDisclosure != "full",
		v.Kind == "context" && !present && field.key == "summary" &&
			(v.Policy.AgentDisclosure == "reference" || v.Policy.AgentDisclosure == "local-tool"):
		muted(c, "Hidden by disclosure policy")
	case !present || value == nil:
		muted(c, "Not set")
	case value == "":
		muted(c, "(empty text)")
	default:
		body, ok := value.(string)
		if !ok {
			data, _ := json.MarshalIndent(value, "", "  ")
			body = string(data)
		}
		if len(body) > 1200 {
			ui.TextArea(c, &body).Label(field.label).ReadOnly(true).Height(148).FillWidth()
		} else {
			ui.Text(c, body).FontSize(fsBody).TextColor(tk.Text).Selectable().LineHeight(1.45)
		}
	}
}

func choiceLabel(choice string, v *app.ConflictVersion) string {
	if choice == "edit" {
		return "Replace document"
	}
	if choice == "local" {
		if v != nil && v.Deleted {
			return "Apply local deletion"
		}
		return "Keep local version"
	}
	if v != nil && v.Deleted {
		return "Apply remote deletion"
	}
	return "Keep remote version"
}

func choiceDescription(choice string, v *app.ConflictVersion) string {
	if choice == "edit" {
		return "Writes new content in the same shared storage, then queues it for publication."
	}
	if v != nil && v.Deleted {
		return "Deletes the record on every device once the new revision is published."
	}
	return "Restores this side's complete visible revision as the new local result."
}

func (d *desktop) conflictResolution(c *ui.Context, p *conflictPage) {
	card(c).Children(func() {
		cardHeader(c, icMerge, "Resolution", "Compare all available evidence before choosing. Nothing is written until confirmation.", nil)
		ui.RadioGroup(c, func() {
			for _, choice := range []string{"local", "remote"} {
				v := p.version(choice)
				ui.Radio(c, &p.choice, choice, choiceLabel(choice, v)).Disabled(p.loading || p.writing || p.review || v == nil)
			}
			if p.editSource() != nil {
				ui.Radio(c, &p.choice, "edit", choiceLabel("edit", nil)).Disabled(p.loading || p.writing || p.review)
			}
		}).Row().Wrap().Gap(sp3).Label("Resolution choice")
		if p.choice != "" {
			muted(c, choiceDescription(p.choice, p.version(p.choice)))
		}
		switch p.choice {
		case "edit":
			if p.draft == nil {
				p.startDraft()
			}
			if p.draft != nil {
				p.editView(c)
			}
		case "local", "remote":
			if v := p.version(p.choice); v != nil {
				d.conflictPreview(c, v)
			}
		default:
			muted(c, "Choose a whole side or replace an eligible Context document.")
		}
		if p.choice != "" && !d.compact {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				confirmToken(c, confirmSpec{tokenLabel: "conflict token", token: p.detail.ConflictID})
				ui.Spacer(c)
				ui.Button(c, "Cancel").Disabled(p.writing).OnClick(d.back)
				ui.PrimaryButton(c, choiceLabel(p.choice, p.version(p.choice))).Label("Confirm " + choiceLabel(p.choice, p.version(p.choice))).Disabled(!p.canConfirm()).OnClick(func() { d.resolveConflict(p) })
			})
			if p.writing {
				muted(c, "Saving the local revision and rereading the Queue…")
			}
		}
	})
}

func (d *desktop) conflictPreview(c *ui.Context, v *app.ConflictVersion) {
	previewPanel(c, "Result preview", choiceDescription(d.conflict.choice, v), []effect{
		{toneAccent, d.conflictDevice(v.Provenance.DeviceID), "Source revision " + v.Revision},
		{tonePending, "Publication", "Saved locally and queued; other devices receive it only after a confirmed commit."},
	}, nil, func() {
		if v.Kind == "context" {
			for _, field := range []comparisonField{{key: "summary", label: "Document summary"}, {key: "body", label: "Body"}} {
				value, present := payload(v)[field.key]
				if field.key == "summary" && !present {
					continue
				}
				conflictValue(c, v, field, value, present)
			}
		}
	})
}

func (p *conflictPage) editView(c *ui.Context) {
	draft := p.draft
	input := &draft.input
	well(c).Disabled(p.writing || p.loading || p.review).Children(func() {
		strong(c, "Replacement body")
		if draft.bodyHidden {
			muted(c, "The original body is hidden by disclosure. This input replaces the complete body.")
		}
		if draft.summaryHidden {
			muted(c, "The original summary is hidden. Enter a new summary under other fields if it should be retained.")
		}
		ui.TextArea(c, &input.Body).Label("Edit replacement body").FillWidth().Height(112)
		ui.Row(c).FillWidth().Gap(sp2).Wrap().Children(func() {
			ui.Text(c, "Agent disclosure").TextColor(tk.Text)
			ui.Select(c, &input.Policy.AgentDisclosure, []string{"full", "summary", "reference", "local-tool"}).
				Label("Select Agent disclosure").MinWidth(110)
			if input.Policy.AgentDisclosure == "" {
				muted(c, "Choose a disclosure range")
			}
		})
		ui.Collapsible(c, "Edit other fields", &p.metadata, func() {
			for _, field := range []struct {
				label string
				value *string
			}{
				{"Title", &input.Title}, {"Purpose", &input.Purpose}, {"Source", &input.Source},
				{"Observed at (RFC 3339)", &draft.observedAt}, {"Valid until (RFC 3339)", &input.ValidUntil},
			} {
				muted(c, field.label)
				ui.TextInput(c, field.value).Label(field.label).FillWidth()
			}
			muted(c, "Document summary")
			ui.TextArea(c, &draft.summary).Label("Document summary").FillWidth().Height(70)
		})
	})
}

func (d *desktop) conflictConfirmation(c *ui.Context, p *conflictPage) {
	if !p.confirm {
		return
	}
	ui.Modal(c, &p.confirm, func() {
		ui.Column(c).Width(420).Gap(sp3).Children(func() {
			section(c, "Confirm local revision")
			strong(c, p.description())
			muted(c, "Resolution · "+choiceLabel(p.choice, p.version(p.choice)))
			confirmToken(c, confirmSpec{tokenLabel: "conflict token", token: p.detail.ConflictID})
			ui.Row(c).Gap(sp2).Justify(ui.End).Children(func() {
				ui.Button(c, "Cancel").AutoFocus().OnClick(func() { p.confirm = false })
				ui.PrimaryButton(c, "Confirm revision").OnClick(func() {
					p.confirm = false
					d.resolveConflict(p)
				})
			})
		})
	})
}
