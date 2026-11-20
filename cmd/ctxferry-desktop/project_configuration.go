package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

var projectLayers = []struct{ key, label string }{
	{"global", "Workspace default"}, {"project", "Project"}, {"profile", "Profile"}, {"device", "Device"}, {"checkout", "Checkout"},
}

func layerLabel(key string) string {
	for _, layer := range projectLayers {
		if layer.key == key {
			return layer.label
		}
	}
	return "—"
}

func scopeText(layer string) string {
	switch layer {
	case "global":
		return "Shared · Projects inheriting the Workspace default"
	case "project":
		return "Shared · devices and Checkouts inheriting this Project layer"
	case "profile":
		return "Shared · Checkouts using this Profile unless Device or Checkout layers override it"
	case "device":
		return "Local only · Checkouts on this device inheriting the Device layer"
	default:
		return "Local only · the selected Checkout"
	}
}

type projectEditor struct {
	open, reset, create bool
	input               string
	preview             *app.ProjectConfigurationPreview
	err                 error
}

type configurationReceiptState struct {
	receipt app.ConfigurationReceipt
	target  app.ProjectConfigurationTarget
	reset   bool
	written time.Time
	read    time.Time
}

func (p *projectPage) configurationView(c *ui.Context) {
	if p.editor.open && !p.editor.create {
		project := p.currentProject()
		if project == nil {
			return
		}
		if p.host != nil && p.host.compact {
			p.editorView(c, *project)
			return
		}
		managementColumns(c, func() { p.fieldTable(c) }, func() { p.editorView(c, *project) })
		return
	}
	ui.Row(c).FillWidth().Gap(sp2).Children(func() {
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			section(c, "Effective Configuration")
			muted(c, "Layers are in precedence order: a later layer overrides an earlier one. The first three are shared; Device and Checkout layers stay on each device.")
		})
		ui.Button(c, "New Profile…").Disabled(p.busy).OnClick(func() {
			p.editor = projectEditor{open: true, create: true}
		})
	})
	managementColumns(c, func() {
		p.fieldTable(c)
		if field := p.selectedField(); field != nil {
			p.layerTable(c, *field)
		}
	}, func() {
		if field := p.selectedField(); field != nil {
			p.configurationInspector(c, *field)
		}
	})
}

func (p *projectPage) fieldTable(c *ui.Context) {
	p.fieldRows.Selected = &p.field
	fields := p.config.Fields
	p.fieldRows.Key = func(row int) any { return fields[row].Name }
	p.fieldRows.Label = func(row int) string { return fields[row].Name }
	card(c).Padding(0).Clip().Children(func() {
		columns := []ui.TableColumn{{Title: "Field", Width: 170}, {Title: "Effective value"}, {Title: "Origin", Width: 120}}
		table := ui.Table(c, &p.fieldRows, columns, len(fields), func(row, col int) {
			field := fields[row]
			switch col {
			case 0:
				ui.Text(c, field.Name).Font(fontMono)
			case 1:
				ui.Text(c, configurationValue(field.Result)).TruncateMiddle().Tooltip(configurationValue(field.Result))
			case 2:
				label := "—"
				if field.Result.Effective != nil {
					label = layerLabel(field.Result.Effective.Origin.Layer)
					if field.Result.Effective.Origin.Source != "" {
						label += " · " + field.Result.Effective.Origin.Source
					}
				}
				ui.Text(c, label)
			}
		}).Height(float32(min(8, len(fields))+1)*31 + 10).FillWidth().Label("Configuration fields")
		if table.Changed() {
			if field := p.selectedField(); field != nil && field.Result.Effective != nil {
				p.layer = field.Result.Effective.Origin.Layer
				p.layerChoice = layerLabel(p.layer)
			}
		}
		listFooter(c, fmt.Sprintf("Showing %d fields from the latest read", len(fields)), nil)
	})
}

func (p *projectPage) selectedField() *app.ProjectConfigurationField {
	if p.field < 0 || p.field >= len(p.config.Fields) {
		return nil
	}
	return &p.config.Fields[p.field]
}

func (p *projectPage) layerTable(c *ui.Context, field app.ProjectConfigurationField) {
	card(c).Padding(sp3, sp4).Children(func() {
		cardHeader(c, icLayers, field.Name+" · layer contributions", "The winning layer supplies the effective value; local layers on other devices are not visible here.", nil)
		inherited := "Not set"
		for _, layer := range projectLayers {
			direct := storedAt(field, layer.key)
			value, status := "Inherits "+inherited, ""
			if inherited == "Not set" {
				value = inherited
			}
			if reason := p.layerUnavailable(layer.key); reason != "" && direct == nil {
				value = reason
			}
			if direct != nil {
				value, status, inherited = formatField(field.Name, direct.Value), "Set here", layer.label
				if field.Result.Effective != nil && field.Result.Effective.Origin.Layer == layer.key {
					status = "Wins"
				}
			}
			row := listRow(c, status == "Wins", func() {
				ui.Text(c, layer.label).Width(128).Shrink(0).TextColor(tk.Muted)
				ui.Text(c, value).Grow(1).MinWidth(0).Multiline(true)
				if status != "" {
					tag(c, status)
				}
			})
			if status == "Wins" {
				row.Label("Winning configuration layer")
			}
		}
	})
}

func (p *projectPage) configurationInspector(c *ui.Context, field app.ProjectConfigurationField) {
	card(c).Children(func() {
		cardHeader(c, icSliders, field.Name, "Effective value and writable layer", nil)
		if field.Result.Effective != nil {
			origin := field.Result.Effective.Origin
			strong(c, configurationValue(field.Result))
			muted(c, "Origin · "+layerLabel(origin.Layer)+" · "+origin.Source)
			if updated, err := time.Parse(time.RFC3339Nano, origin.UpdatedAt); err == nil {
				muted(c, "Updated "+updated.Local().Format("2006-01-02 15:04"))
			}
		} else {
			muted(c, "Not set")
		}
		if field.Result.Problem != "" {
			banner(c, toneAttention, "Effective value problem", configurationProblem(field.Result.Problem), nil)
		}
		ui.Divider(c)
		ui.Text(c, "Editing layer").FontSize(fsMeta).TextColor(tk.Muted)
		p.layerPicker(c)
		muted(c, scopeText(p.layer))
		if reason := p.layerUnavailable(p.layer); reason != "" {
			muted(c, reason)
			return
		}
		direct := storedAt(field, p.layer)
		if direct == nil {
			muted(c, "No direct value at this layer")
		} else {
			detail(c, "Set at this layer", formatField(field.Name, direct.Value))
		}
		ui.PrimaryButton(c, "Preview edit to "+layerLabel(p.layer)+"…").FillWidth().Disabled(p.busy).OnClick(func() {
			p.editor = projectEditor{open: true}
			if direct != nil {
				p.editor.input = fmt.Sprint(direct.Value)
			}
		})
		if direct != nil {
			fallback := field.WithoutLayer[p.layer]
			detail(c, "After removal", configurationValue(fallback))
			ui.Button(c, "Preview removal from "+layerLabel(p.layer)+"…").FillWidth().Disabled(p.busy).OnClick(func() {
				p.editor = projectEditor{open: true, reset: true}
				p.previewChange(field.Name)
			})
		}
	})
}

func (p *projectPage) layerPicker(c *ui.Context) {
	labels := make([]string, 0, len(projectLayers))
	for _, layer := range projectLayers {
		labels = append(labels, layer.label)
	}
	if p.layerChoice == "" {
		p.layerChoice = layerLabel(p.layer)
	}
	if ui.Select(c, &p.layerChoice, labels).MinWidth(0).FillWidth().Label("Select configuration write layer").Disabled(p.busy).Changed() {
		for _, layer := range projectLayers {
			if layer.label == p.layerChoice {
				p.layer = layer.key
				break
			}
		}
	}
}

func (p *projectPage) layerUnavailable(layer string) string {
	if layer == "profile" && p.config.Profile == nil {
		if p.target.CheckoutID == "" {
			return "Choose a local Checkout first"
		}
		return "This Checkout has no saved Profile"
	}
	if layer == "checkout" {
		switch p.config.CheckoutProblem {
		case "checkout-unavailable":
			return "The bound directory is missing or now points elsewhere"
		case "checkout-required":
			return "Choose a local Checkout first"
		}
	}
	return ""
}

func storedAt(field app.ProjectConfigurationField, layer string) *app.EffectiveValue {
	i := slices.IndexFunc(field.Layers, func(v app.EffectiveValue) bool { return v.Origin.Layer == layer })
	if i < 0 {
		return nil
	}
	return &field.Layers[i]
}

func formatField(name string, value any) string {
	if name == "timeoutSeconds" {
		return fmt.Sprint(value) + " seconds"
	}
	return fmt.Sprint(value)
}

func configurationValue(v app.ProjectConfigurationValue) string {
	if v.Problem != "" {
		return "Not applicable"
	}
	if v.Effective == nil {
		return "Not set"
	}
	return formatField(v.Effective.Field, v.Effective.Value)
}

func configurationProblem(problem string) string {
	switch problem {
	case "the bound checkout directory is missing or now points to another directory":
		return "The bound directory is missing or now points elsewhere."
	case "run from a local checkout of this Project":
		return "A relative working directory requires a selected local Checkout."
	case "workingDirectory is not valid on this device":
		return "The working-directory path is invalid on this device."
	case "effective workingDirectory must exist and be a directory on this device":
		return "The working directory does not exist or is not a directory on this device."
	case "shared workingDirectory must resolve within the checkout":
		return "The shared working directory must resolve inside the selected Checkout."
	default:
		return problem
	}
}

func (p *projectPage) previewChange(field string) {
	change := app.ConfigurationChange{Layer: p.layer, Field: field}
	if p.layer == "profile" {
		change.Profile = p.config.Profile.Name
	}
	if !p.editor.reset {
		if field == "workingDirectory" {
			change.Value, _ = json.Marshal(p.editor.input)
		} else {
			change.Value = json.RawMessage(p.editor.input)
		}
	}
	target := p.target
	p.editor.err = nil
	p.work(func() func() {
		preview, err := p.core.PreviewProjectConfiguration(p.ctx, target, change)
		return func() {
			p.editor.err = err
			if err == nil {
				p.editor.preview = &preview
			}
		}
	})
}

func (p *projectPage) editorView(c *ui.Context, project app.DesktopProject) {
	e := &p.editor
	if !e.open {
		return
	}
	if e.create {
		card(c).Children(func() {
			cardHeader(c, icLayers, "Create a shared Profile", "Project "+project.Slug+" · shared Configuration", nil)
			ui.TextInput(c, &e.input).FillWidth().Label("New Profile name").Disabled(p.busy)
			p.editorActions(c, "Create shared Profile", e.input == "", p.createProfile)
		})
		return
	}
	field := p.selectedField()
	if field == nil {
		return
	}
	if e.preview == nil {
		title := "Edit Configuration"
		if e.reset {
			title = "Remove layer override"
		}
		card(c).Children(func() {
			cardHeader(c, icSliders, title+" · "+field.Name, scopeText(p.layer), nil)
			muted(c, "Project "+project.Slug+" · editing layer "+layerLabel(p.layer))
			if p.target.CheckoutID != "" {
				muted(c, "Checkout "+p.checkoutChoice+" · "+p.target.CheckoutID)
			}
			if !e.reset {
				ui.TextInput(c, &e.input).Label("New configuration value").FillWidth().Disabled(p.busy)
				if field.Name == "workingDirectory" {
					muted(c, "Use forward slashes for relative paths. Device and Checkout layers also accept local absolute paths.")
				}
				p.editorActions(c, "Preview change", e.input == "", func() { p.previewChange(field.Name) })
			} else if e.err != nil {
				projectError(c, e.err)
				p.editorActions(c, "Retry preview", false, func() { p.previewChange(field.Name) })
			} else {
				muted(c, "Building the scope-aware preview…")
			}
		})
		return
	}
	preview := e.preview
	heading := "Preview edit to " + layerLabel(p.layer)
	if e.reset {
		heading = "Preview removal from " + layerLabel(p.layer)
	}
	effects := []effect{{toneAccent, field.Name, configurationValue(preview.Before) + " → " + configurationValue(preview.After)}}
	for _, item := range preview.Checkouts {
		target := item.Checkout.Path
		if preview.Change.Layer == "global" {
			target = item.Project.Slug + " · " + target
		}
		result := "Retains " + configurationValue(item.After)
		t := toneNeutral
		if item.Changed {
			result = configurationValue(item.Before) + " → " + configurationValue(item.After)
			t = toneAccent
		}
		if item.Unknown != "" {
			result, t = "Currently unknown · "+item.Unknown, toneAttention
		}
		effects = append(effects, effect{t, target, result})
	}
	if preview.Change.Layer == "global" || preview.Change.Layer == "project" || preview.Change.Layer == "profile" {
		effects = append(effects, effect{tonePending, "Other devices", "Inherit the shared value only after a confirmed commit, unless their own Device or Checkout layer overrides it. Those local layers are not visible here."})
		effects = append(effects, effect{tonePending, "Publication", "Queues one shared record. Published only when a confirmed commit includes it."})
	}
	previewPanel(c, heading, "Project "+project.Slug+" · "+scopeText(p.layer), effects, nil, func() {
		if preview.After.Problem != "" {
			banner(c, toneAttention, "Result needs attention", configurationProblem(preview.After.Problem), nil)
		}
		if p.host == nil || !p.host.compact {
			confirmToken(c, confirmSpec{tokenLabel: "preview token", token: preview.PreviewID})
			p.editorActions(c, map[bool]string{true: "Remove from " + layerLabel(p.layer), false: "Save to " + layerLabel(p.layer)}[e.reset], false, p.applyChange)
		}
	})
}

func (p *projectPage) editorActions(c *ui.Context, label string, disabled bool, action func()) {
	if p.editor.err != nil {
		projectError(c, p.editor.err)
	}
	ui.Row(c).FillWidth().Gap(sp2).Justify(ui.End).Children(func() {
		ui.Button(c, "Cancel").Disabled(p.busy).OnClick(func() { p.editor.open = false })
		ui.PrimaryButton(c, label).Disabled(disabled || p.busy).OnClick(action)
	})
}

func (p *projectPage) applyChange() {
	if p.host != nil && p.host.err != nil {
		return
	}
	preview := *p.editor.preview
	p.work(func() func() {
		receipt, err := p.core.ApplyProjectConfiguration(p.ctx, preview)
		return func() {
			if err != nil {
				p.editor.err = err
				p.editor.preview = nil
				return
			}
			p.notice = configurationReceipt(receipt, preview.Change.Value == nil)
			p.receipt = &configurationReceiptState{receipt: receipt, target: preview.Target, reset: preview.Change.Value == nil, written: time.Now()}
			p.readSelection()
		}
	})
}

func (p *projectPage) createProfile() {
	if p.host != nil && p.host.err != nil {
		return
	}
	target, name := p.target, p.editor.input
	p.work(func() func() {
		_, err := p.core.CreateProjectProfile(p.ctx, target.ProjectID, name)
		return func() {
			if err != nil {
				p.editor.err = err
				return
			}
			p.notice = "Created shared Profile " + name + ". Select it explicitly for a Checkout."
			p.readSelection()
		}
	})
}

func configurationReceipt(receipt app.ConfigurationReceipt, reset bool) string {
	action := "Saved"
	if reset {
		action = "Removed override"
	}
	storage := "local only"
	if receipt.Storage == "shared" {
		storage = "saved locally and queued for publication"
	}
	return action + " · " + receipt.Field + " · " + layerLabel(receipt.Layer) + " layer · " + storage
}

func (p *projectPage) configurationReceiptView(c *ui.Context) {
	r := p.receipt
	if r == nil || r.target != p.target {
		return
	}
	published, publication, t := "Not applicable", "Local-only change; nothing is shared", toneNeutral
	if r.receipt.Storage == "shared" {
		published, publication, t = "Not yet", "Queued locally; only a confirmed commit publishes it", tonePending
	}
	read := "Reload pending"
	if !r.read.IsZero() {
		read = "Reread " + r.read.Local().Format("15:04:05")
	}
	receiptPanel(c, receipt{
		heading: configurationReceipt(r.receipt, r.reset),
		rows: []kvRow{
			{k: "Field", v: r.receipt.Field, mono: true},
			{k: "Layer", v: layerLabel(r.receipt.Layer)},
			{k: "Scope", v: scopeText(r.receipt.Layer)},
			{k: "Record ID", v: valueOr(r.receipt.ID, "Local-only record"), mono: true},
		},
		ev: evidence{
			written: "Saved " + r.written.Local().Format("15:04:05"), writtenDetail: valueOr(r.receipt.ID, "Local-only write"),
			read: read, readDetail: "Effective values reloaded for the same Project and Checkout",
			published: published, pubDetail: publication, pubTone: t,
		},
	})
}
