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

var conflictBorder = ui.Hex("#cedbd4")

const conflictReceipt = "已生成本机修订"

func (d *desktop) conflictQueueRow(c *ui.Context, id string) {
	label := d.data.ConflictLabels[id]
	if label == "" {
		label = id
	}
	ui.Row(c).Key(id).FillWidth().Gap(12).Padding(8, 12).Radius(6).Background(panel).Children(func() {
		ui.Text(c, "冲突").TextColor(muted)
		ui.Text(c, label).Grow(1).MinWidth(0).Bold().TextColor(ink).Selectable()
		button := ui.Button(c, "比较版本").Label("比较版本 · " + id)
		if d.restoreQueueID == id {
			button.Focus()
		}
		if button.Clicked() {
			d.openConflict(id)
		}
	})
}

func (d *desktop) conflictView(c *ui.Context) {
	p := d.conflict
	ui.Row(c).FillWidth().Gap(12).Padding(10, 20).Children(func() {
		if ui.Button(c, "返回同步队列").Disabled(p.writing).Clicked() {
			d.conflict = nil
			d.restoreQueueID = p.id
			if p.review {
				d.refresh()
			}
		}
		ui.Text(c, "本地工作区 / 同步 / 冲突").Grow(1).MinWidth(0).TextColor(muted)
		if p.revision == "" && ui.Button(c, "刷新比较").Disabled(p.loading || p.writing).Clicked() {
			d.loadConflict()
		}
	})
	ui.Divider(c)
	ui.Scroll(c).Key(p.id).TrackScroll(&p.scroll).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(14).Padding(20, 24).Children(func() {
			ui.Text(c, "冲突处理").FontSize(27).Bold().TextColor(ink)
			if p.revision != "" {
				ui.Column(c).Gap(6).Padding(16).Radius(6).Background(panel).Children(func() {
					section(c, conflictReceipt)
					ui.Text(c, p.resolvedTitle()).Bold()
					ui.Text(c, "记录 ID "+p.id).Font("monospace").Selectable()
					ui.Text(c, "本机修订 "+p.revision).Font("monospace").Selectable()
					ui.Text(c, "修订已加入本机发布队列，由后续同步发布。").TextColor(muted)
				})
				return
			}
			if p.loading {
				ui.Text(c, "正在读取三方版本…")
			}
			if p.err != "" {
				ui.Text(c, p.err).Padding(10, 12).Radius(6).Background(ui.Hex("#fbf1df")).TextColor(ui.Hex("#614717"))
			}
			if p.detail.ConflictID == "" {
				return
			}
			ui.Text(c, p.description()).TextColor(muted)
			d.conflictComparison(c, p)
			d.conflictResolution(c, p)
		})
	})
	d.conflictConfirmation(c, p)
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
	kind := "共享记录"
	if v := p.detail.Local; v != nil {
		kind = v.Kind
		if kind == "context" {
			kind = "Context 文档"
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
		"record.deleted": "有效",
	}
	if v.Deleted {
		fields["record.deleted"] = "已删除"
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
		"body": "正文", "summary": "文档摘要", "title": "标题", "purpose": "用途",
		"source": "来源", "observedAt": "观察时间", "validUntil": "有效期",
		"policy.agentDisclosure": "Agent 披露范围", "policy.storage": "存储范围", "record.deleted": "记录状态",
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
	names := [3]string{"共同基线", "本机版本", "远端版本"}
	choices := [3]string{"", "local", "remote"}
	maps := [3]map[string]any{versionFields(versions[0]), versionFields(versions[1]), versionFields(versions[2])}
	fields := comparisonFields(maps)
	changed := 0
	for _, field := range fields {
		if field.changed {
			changed++
		}
	}
	ui.Row(c).FillWidth().Gap(10).Wrap().Children(func() {
		section(c, "三方比较")
		ui.Text(c, fmt.Sprintf("%d 个可见字段有差异", changed)).TextColor(muted)
		ui.Spacer(c)
		ui.Checkbox(c, &p.all, "显示所有字段")
	})
	width, _ := c.Size()
	narrow := width < 980
	cards := ui.Row(c).FillWidth().AlignItems(ui.Stretch).Gap(12)
	if narrow {
		cards.Column()
	}
	cards.Children(func() {
		for i, v := range versions {
			border := conflictBorder
			selected := p.choice != "" && p.choice == choices[i]
			if selected {
				border = ink
			}
			card := ui.Column(c).Key(names[i]).MinWidth(0).Gap(8).Padding(14).Border(1, border).Radius(8)
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
	ui.Row(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, name).FontSize(17).Bold().TextColor(ink)
		if selected {
			ui.Text(c, "已选").Padding(3, 8).Radius(4).Background(panel).TextColor(ink)
		}
	})
	if v == nil {
		ui.Text(c, "此版本不存在").TextColor(muted)
		return
	}
	ui.Text(c, d.conflictDevice(v.Provenance.DeviceID)).TextColor(ink).Selectable()
	ui.Text(c, v.Provenance.DeviceID).Font("monospace").FontSize(11).TextColor(muted).Selectable()
	ui.Text(c, conflictTime(v.Provenance.UpdatedAt)).TextColor(muted)
	ui.Text(c, "修订 "+v.Revision).Font("monospace").FontSize(11).TextColor(muted).Selectable()
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
			label += " · 有差异"
		}
		ui.Text(c, label).FontSize(13).Bold().TextColor(muted)
		conflictValue(c, v, field, value, present)
	}
	if v.Kind == "context" && v.Policy.AgentDisclosure != "full" {
		ui.Text(c, "正文按披露策略隐藏").FontSize(13).TextColor(muted)
	}
}

func (d *desktop) conflictDevice(id string) string {
	if id == "" {
		return "设备未知"
	}
	if local := d.data.Device; id == local.ID && local.Name != "" {
		return local.Name + " · " + local.OS + "/" + local.Arch
	}
	return "设备环境未知"
}

func conflictTime(raw string) string {
	stamp, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	stamp = stamp.Local()
	if stamp.Format("2006-01-02") == time.Now().Format("2006-01-02") {
		return "今天 " + stamp.Format("15:04:05")
	}
	return stamp.Format("2006-01-02 15:04:05")
}

func conflictValue(c *ui.Context, v *app.ConflictVersion, field comparisonField, value any, present bool) {
	switch {
	case v.Kind == "context" && !present && field.key == "body" && v.Policy.AgentDisclosure != "full",
		v.Kind == "context" && !present && field.key == "summary" &&
			(v.Policy.AgentDisclosure == "reference" || v.Policy.AgentDisclosure == "local-tool"):
		ui.Text(c, "按披露策略隐藏").TextColor(muted)
	case !present || value == nil:
		ui.Text(c, "未设置").TextColor(muted)
	case value == "":
		ui.Text(c, "（空文本）").TextColor(muted)
	default:
		body, ok := value.(string)
		if !ok {
			data, _ := json.MarshalIndent(value, "", "  ")
			body = string(data)
		}
		if len(body) > 1200 {
			ui.TextArea(c, &body).Label(field.label).ReadOnly(true).Height(148).FillWidth()
		} else {
			ui.Text(c, body).FontSize(15).TextColor(ink).Selectable()
		}
	}
}

func choiceLabel(choice string, v *app.ConflictVersion) string {
	if choice == "edit" {
		return "编辑新版本"
	}
	if choice == "local" {
		if v != nil && v.Deleted {
			return "保留本机删除"
		}
		return "采用本机"
	}
	if v != nil && v.Deleted {
		return "保留远端删除"
	}
	return "采用远端"
}

func (d *desktop) conflictResolution(c *ui.Context, p *conflictPage) {
	ui.Column(c).FillWidth().Gap(12).Padding(16).Radius(8).Border(1, conflictBorder).Background(panel).Children(func() {
		ui.Row(c).FillWidth().Gap(16).Wrap().Children(func() {
			section(c, "解决方式")
			ui.RadioGroup(c, func() {
				for _, choice := range []string{"local", "remote"} {
					v := p.version(choice)
					ui.Radio(c, &p.choice, choice, choiceLabel(choice, v)).Disabled(p.loading || p.writing || p.review || v == nil)
				}
				if p.editSource() != nil {
					ui.Radio(c, &p.choice, "edit", choiceLabel("edit", nil)).Disabled(p.loading || p.writing || p.review)
				}
			}).Row().Wrap().Gap(12).Label("解决方式")
		})
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
			ui.Text(c, "比较三方证据后选择要保存的结果。").TextColor(muted)
		}
		ui.Row(c).FillWidth().Gap(12).Wrap().Children(func() {
			if ui.PrimaryButton(c, "生成本机修订…").Disabled(!p.canConfirm()).Clicked() {
				p.confirm = true
			}
			label := "保存到当前设备，等待后续发布。"
			if p.writing {
				label = "正在保存本机修订…"
			}
			ui.Text(c, label).TextColor(muted)
		})
	})
}

func (d *desktop) conflictPreview(c *ui.Context, v *app.ConflictVersion) {
	ui.Column(c).FillWidth().Gap(5).Padding(12).Radius(5).Background(c.Theme().Background).Children(func() {
		ui.Text(c, "结果预览").Bold().TextColor(ink)
		if v.Deleted {
			ui.Text(c, "保留删除。新修订将把此记录标记为已删除。").TextColor(ink)
			return
		}
		ui.Text(c, "来源 · "+d.conflictDevice(v.Provenance.DeviceID)).TextColor(muted)
		ui.Text(c, v.Provenance.DeviceID).Font("monospace").FontSize(11).TextColor(muted).Selectable()
		if v.Kind == "context" {
			for _, field := range []comparisonField{{key: "summary", label: "文档摘要"}, {key: "body", label: "正文"}} {
				value, present := payload(v)[field.key]
				if field.key == "summary" && !present {
					continue
				}
				conflictValue(c, v, field, value, present)
			}
		} else {
			ui.Text(c, "将采用此侧的全部字段。可在三方比较中查看。").TextColor(ink)
		}
	})
}

func (p *conflictPage) editView(c *ui.Context) {
	draft := p.draft
	input := &draft.input
	ui.Column(c).FillWidth().Gap(8).Padding(12).Radius(5).Background(c.Theme().Background).Disabled(p.writing || p.loading || p.review).Children(func() {
		ui.Text(c, "结果正文").Bold().TextColor(ink)
		if draft.bodyHidden {
			ui.Text(c, "原正文按披露策略隐藏。此处输入将完整替换正文。").TextColor(muted)
		}
		if draft.summaryHidden {
			ui.Text(c, "原摘要按披露策略隐藏。请在其他字段中输入要保留的新摘要。").TextColor(muted)
		}
		ui.TextArea(c, &input.Body).Label("编辑结果正文").FillWidth().Height(88)
		ui.Row(c).FillWidth().Gap(10).Wrap().Children(func() {
			ui.Text(c, "Agent 披露范围").TextColor(ink)
			ui.Select(c, &input.Policy.AgentDisclosure, []string{"full", "summary", "reference", "local-tool"}).
				Label("选择 Agent 披露范围").MinWidth(110)
			if input.Policy.AgentDisclosure == "" {
				ui.Text(c, "请选择披露范围").TextColor(ink)
			}
		})
		ui.Collapsible(c, "编辑其他字段", &p.metadata, func() {
			for _, field := range []struct {
				label string
				value *string
			}{
				{"标题", &input.Title}, {"用途", &input.Purpose}, {"来源", &input.Source},
				{"观察时间（RFC 3339）", &draft.observedAt}, {"有效期（RFC 3339）", &input.ValidUntil},
			} {
				ui.Text(c, field.label).TextColor(muted)
				ui.TextInput(c, field.value).Label(field.label).FillWidth()
			}
			ui.Text(c, "文档摘要").TextColor(muted)
			ui.TextArea(c, &draft.summary).Label("文档摘要").FillWidth().Height(70)
		})
	})
}

func (d *desktop) conflictConfirmation(c *ui.Context, p *conflictPage) {
	if !p.confirm {
		return
	}
	ui.Modal(c, &p.confirm, func() {
		ui.Column(c).Width(420).Gap(12).Children(func() {
			section(c, "确认生成本机修订")
			ui.Text(c, p.description()).Bold().TextColor(ink)
			ui.Text(c, "方案 · "+choiceLabel(p.choice, p.version(p.choice))).TextColor(ink)
			ui.Text(c, "写入设备 · "+d.conflictDevice(d.data.Device.ID)).TextColor(ink)
			ui.Text(c, "结果将保存在本机并加入发布队列。发布后，共享内容将以明文保留在 Git 历史中。").TextColor(muted)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").AutoFocus().Clicked() {
					p.confirm = false
				}
				if ui.PrimaryButton(c, "确认生成").Clicked() {
					p.confirm = false
					d.resolveConflict(p)
				}
			})
		})
	})
}
