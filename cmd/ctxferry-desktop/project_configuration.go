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
	{"global", "全局"}, {"project", "项目"}, {"profile", "配置方案"}, {"device", "设备"}, {"checkout", "Checkout"},
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
		return "共享 · 工作区中继承全局值的项目"
	case "project":
		return "共享 · 继承项目层的设备与 Checkout"
	case "profile":
		return "共享 · 选择此方案并继承方案层的 Checkout"
	case "device":
		return "仅本机 · 此项目中继承设备层的 Checkout"
	default:
		return "仅本机 · 所选 Checkout"
	}
}

type projectEditor struct {
	open, reset, create bool
	input               string
	preview             *app.ProjectConfigurationPreview
	err                 error
}

func (p *projectPage) configurationView(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		section(c, "普通配置")
		ui.Spacer(c)
		if ui.Button(c, "新建配置方案…").Disabled(p.busy).Clicked() {
			p.editor = projectEditor{open: true, create: true}
		}
	})
	width, _ := c.Size()
	column := func() {
		p.fieldTable(c)
		if field := p.selectedField(); field != nil {
			p.layerTable(c, *field)
		}
	}
	inspector := func() {
		if field := p.selectedField(); field != nil {
			p.configurationInspector(c, *field)
		}
	}
	if width >= 1050 {
		ui.Row(c).FillWidth().Gap(18).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(12).Children(column)
			ui.Column(c).Width(280).Shrink(0).Children(inspector)
		})
	} else {
		column()
		inspector()
	}
}

func (p *projectPage) fieldTable(c *ui.Context) {
	p.fieldRows.Selected = &p.field
	fields := p.config.Fields
	p.fieldRows.Key = func(row int) any { return fields[row].Name }
	columns := []ui.TableColumn{{Title: "字段", Width: 170}, {Title: "生效值"}, {Title: "来源", Width: 80}}
	table := ui.Table(c, &p.fieldRows, columns, len(fields), func(row, col int) {
		field := fields[row]
		switch col {
		case 0:
			ui.Text(c, field.Name).Font("monospace")
		case 1:
			ui.Text(c, configurationValue(field.Result)).TruncateMiddle()
		case 2:
			label := "—"
			if field.Result.Effective != nil {
				label = layerLabel(field.Result.Effective.Origin.Layer)
			}
			ui.Text(c, label)
		}
	}).Height(130).FillWidth().Label("普通配置字段")
	if table.Changed() {
		if field := p.selectedField(); field != nil && field.Result.Effective != nil {
			p.layer = field.Result.Effective.Origin.Layer
			p.layerChoice = layerLabel(p.layer)
		}
	}
}

func (p *projectPage) selectedField() *app.ProjectConfigurationField {
	if p.field < 0 || p.field >= len(p.config.Fields) {
		return nil
	}
	return &p.config.Fields[p.field]
}

func (p *projectPage) layerTable(c *ui.Context, field app.ProjectConfigurationField) {
	section(c, field.Name+" · 取值来源")
	inherited := "未设置"
	for _, layer := range projectLayers {
		direct := storedAt(field, layer.key)
		value, status := "继承 "+inherited, ""
		if inherited == "未设置" {
			value = inherited
		}
		if reason := p.layerUnavailable(layer.key); reason != "" && direct == nil {
			value = reason
		}
		if direct != nil {
			value, status, inherited = formatField(field.Name, direct.Value), "直接设置", layer.label
			if field.Result.Effective != nil && field.Result.Effective.Origin.Layer == layer.key {
				status = "当前来源"
			}
		}
		row := ui.Row(c).FillWidth().Gap(10).Padding(6, 8)
		if status == "当前来源" {
			row.Background(panel)
		}
		row.Children(func() {
			ui.Text(c, layer.label).Width(80).Shrink(0).TextColor(muted)
			ui.Text(c, value).Grow(1).MinWidth(0).Multiline(true)
			if status != "" {
				ui.Text(c, status).TextColor(ink).Shrink(0)
			}
		})
	}
}

func (p *projectPage) configurationInspector(c *ui.Context, field app.ProjectConfigurationField) {
	ui.Column(c).FillWidth().Gap(10).Padding(14).Background(panel).Radius(7).Children(func() {
		section(c, field.Name)
		if field.Result.Effective != nil {
			origin := field.Result.Effective.Origin
			ui.Text(c, "来源 · "+layerLabel(origin.Layer)).TextColor(muted)
			if updated, err := time.Parse(time.RFC3339Nano, origin.UpdatedAt); err == nil {
				ui.Text(c, "更新于 "+updated.Local().Format("2006-01-02 15:04")).TextColor(muted).FontSize(12)
			}
		}
		if field.Result.Problem != "" {
			ui.Text(c, configurationProblem(field.Result.Problem)).Multiline(true).TextColor(ui.Hex("#99412d"))
		}
		ui.Divider(c)
		ui.Text(c, "写入层").TextColor(muted)
		p.layerPicker(c)
		ui.Text(c, scopeText(p.layer)).Multiline(true).TextColor(muted)
		if reason := p.layerUnavailable(p.layer); reason != "" {
			ui.Text(c, reason).Multiline(true).TextColor(muted)
			return
		}
		direct := storedAt(field, p.layer)
		if direct == nil {
			ui.Text(c, "该层无直接设置").TextColor(muted)
		} else {
			ui.Text(c, "该层设置 · "+formatField(field.Name, direct.Value)).Multiline(true)
		}
		if ui.PrimaryButton(c, "编辑"+layerLabel(p.layer)+"层…").FillWidth().Disabled(p.busy).Clicked() {
			p.editor = projectEditor{open: true}
			if direct != nil {
				p.editor.input = fmt.Sprint(direct.Value)
			}
		}
		if direct != nil {
			ui.Divider(c)
			fallback := field.WithoutLayer[p.layer]
			ui.Text(c, "恢复后 · "+configurationValue(fallback)).Bold().Multiline(true)
			if fallback.Effective != nil {
				ui.Text(c, "来源 · "+layerLabel(fallback.Effective.Origin.Layer)).TextColor(muted)
			}
			if ui.Button(c, "预览恢复继承…").FillWidth().Disabled(p.busy).Clicked() {
				p.editor = projectEditor{open: true, reset: true}
				p.previewChange(field.Name)
			}
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
	if ui.Select(c, &p.layerChoice, labels).MinWidth(0).FillWidth().Label("选择配置写入层").Disabled(p.busy).Changed() {
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
			return "请先选择本机 Checkout"
		}
		return "此 Checkout 未选择配置方案"
	}
	if layer == "checkout" {
		switch p.config.CheckoutProblem {
		case "checkout-unavailable":
			return "本机绑定目录已丢失或已指向其他位置"
		case "checkout-required":
			return "请先选择本机 Checkout"
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
		return fmt.Sprint(value) + " 秒"
	}
	return fmt.Sprint(value)
}

func configurationValue(v app.ProjectConfigurationValue) string {
	if v.Problem != "" {
		return "不适用"
	}
	if v.Effective == nil {
		return "未设置"
	}
	return formatField(v.Effective.Field, v.Effective.Value)
}

func configurationProblem(problem string) string {
	switch problem {
	case "the bound checkout directory is missing or now points to another directory":
		return "绑定目录已丢失或已指向其他位置。"
	case "run from a local checkout of this Project":
		return "相对工作目录需要已选择的本机 Checkout。"
	case "workingDirectory is not valid on this device":
		return "工作目录路径格式在本机不可用。"
	case "effective workingDirectory must exist and be a directory on this device":
		return "工作目录在本机不存在或不是目录。"
	case "shared workingDirectory must resolve within the checkout":
		return "共享工作目录必须位于所选 Checkout 内。"
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
	ui.Modal(c, &e.open, func() {
		_, height := c.Size()
		ui.Scroll(c).Width(490).MaxHeight(height - 80).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				if e.create {
					section(c, "新建配置方案")
					ui.Text(c, "项目 · "+project.Slug+"　共享配置").Multiline(true)
					ui.TextInput(c, &e.input).FillWidth().Label("新方案名称").Disabled(p.busy)
					p.editorActions(c, "创建共享配置方案", e.input == "", p.createProfile)
					return
				}
				field := p.selectedField()
				if field == nil {
					return
				}
				title := "编辑配置"
				if e.reset {
					title = "恢复继承"
				}
				section(c, title+" · "+field.Name)
				ui.Text(c, "项目 · "+project.Slug+"　写入层 · "+layerLabel(p.layer)).Multiline(true)
				ui.Text(c, scopeText(p.layer)).TextColor(muted).Multiline(true)
				if p.device != "" {
					ui.Text(c, "查看设备 · "+p.device).TextColor(muted).Multiline(true)
				}
				if p.config.Profile != nil {
					ui.Text(c, "配置方案 · "+p.config.Profile.Name).Multiline(true)
				}
				if p.target.CheckoutID != "" {
					ui.Text(c, "Checkout · "+p.checkoutChoice).TruncateMiddle()
				}
				if e.preview == nil && !e.reset {
					ui.TextInput(c, &e.input).Label("新配置值").FillWidth().Disabled(p.busy)
					if field.Name == "workingDirectory" {
						ui.Text(c, "相对路径使用正斜线。设备层和 Checkout 层也接受本机绝对路径。").TextColor(muted).Multiline(true)
					}
					p.editorActions(c, "预览变更", e.input == "", func() { p.previewChange(field.Name) })
					return
				}
				if e.preview != nil {
					preview := e.preview
					if e.reset {
						ui.Text(c, "将移除此字段在所选层的直接设置。").Multiline(true)
					} else {
						ui.Text(c, "保存到该层 · "+formatField(field.Name, preview.Stored)).Multiline(true)
					}
					ui.Text(c, configurationValue(preview.Before)+" → "+configurationValue(preview.After)).
						FontSize(18).Bold().TextColor(ink).Multiline(true)
					if value := preview.After.Effective; value != nil {
						ui.Text(c, "生效来源 · "+layerLabel(value.Origin.Layer)).TextColor(muted)
					}
					if preview.After.Problem != "" {
						ui.Text(c, configurationProblem(preview.After.Problem)).Multiline(true).TextColor(ui.Hex("#99412d"))
					}
					p.checkoutImpact(c, *preview)
					confirm := "确认保存"
					if e.reset {
						confirm = "确认恢复继承"
					}
					p.editorActions(c, confirm, false, p.applyChange)
				} else {
					p.editorActions(c, "重新预览", false, func() { p.previewChange(field.Name) })
				}
			})
		})
	})
}

func (p *projectPage) checkoutImpact(c *ui.Context, preview app.ProjectConfigurationPreview) {
	ui.Divider(c)
	ui.Text(c, "本机 Checkout 的预览结果").Bold().TextColor(ink)
	if len(preview.Checkouts) == 0 {
		ui.Text(c, "当前范围内没有已订阅项目的本机绑定。").TextColor(muted).Multiline(true)
	} else {
		ui.Scroll(c).FillWidth().Height(min(140, float32(len(preview.Checkouts))*43)).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for _, item := range preview.Checkouts {
					ui.Column(c).FillWidth().Padding(3, 7).Gap(2).Background(panel).Radius(4).Children(func() {
						label := item.Checkout.Path
						if preview.Change.Layer == "global" {
							label = item.Project.Slug + " · " + label
						}
						ui.Text(c, label).TruncateMiddle().FontSize(12)
						result := "保留 " + configurationValue(item.After)
						if item.Changed {
							result = configurationValue(item.Before) + " → " + configurationValue(item.After)
						}
						if value := item.After.Effective; value != nil {
							result += " · " + layerLabel(value.Origin.Layer)
						}
						if item.Unknown != "" {
							result = "当前无法判定 · " + item.Unknown
						}
						ui.Text(c, result).TextColor(ink).FontSize(13).Multiline(true)
					})
				}
			})
		})
	}
	switch preview.Change.Layer {
	case "global", "project", "profile":
		ui.Text(c, "发布后影响采用该共享值的 Checkout。其他设备的本机覆盖需在各自设备核对。").
			TextColor(muted).Multiline(true).FontSize(12)
	}
}

func (p *projectPage) editorActions(c *ui.Context, label string, disabled bool, action func()) {
	if p.editor.err != nil {
		projectError(c, p.editor.err)
	}
	ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c, "取消").Disabled(p.busy).Clicked() {
			p.editor.open = false
		}
		if ui.PrimaryButton(c, label).Disabled(disabled || p.busy).Clicked() {
			action()
		}
	})
}

func (p *projectPage) applyChange() {
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
			p.readSelection()
		}
	})
}

func (p *projectPage) createProfile() {
	target, name := p.target, p.editor.input
	p.work(func() func() {
		_, err := p.core.CreateProjectProfile(p.ctx, target.ProjectID, name)
		return func() {
			if err != nil {
				p.editor.err = err
				return
			}
			p.notice = "已创建共享配置方案 " + name + "。可在 Checkout 上选择该方案。"
			p.readSelection()
		}
	})
}

func configurationReceipt(receipt app.ConfigurationReceipt, reset bool) string {
	action := "已保存"
	if reset {
		action = "已恢复继承"
	}
	storage := "仅本机"
	if receipt.Storage == "shared" {
		storage = "已写入本机共享目录，可从同步页查看发布状态"
	}
	return action + " · " + receipt.Field + " · " + layerLabel(receipt.Layer) + "层 · " + storage
}
