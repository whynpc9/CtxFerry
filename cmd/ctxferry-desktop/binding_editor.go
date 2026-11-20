package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/bitwarden"
	"github.com/whynpc9/CtxFerry/internal/provider/infisical"
)

const newBindingID = "new"

type bindingDraft struct {
	choice    string
	change    app.SecretBindingChange
	issue     string
	confirmed app.SecretBindingChange
}

func newBindingDraft(slot app.DesktopCredentialSlot) *bindingDraft {
	draft := &bindingDraft{choice: newBindingID}
	if len(slot.Reusable) > 0 {
		draft.choice = slot.Reusable[0].ID
	}
	if slot.Status.Binding != nil {
		draft.choice = slot.Status.Binding.ID
	}
	draft.change.ProviderProfile = provider.Profile{Provider: infisical.Name, Name: "default", Method: "personal"}
	draft.change.Locator.Path = "/"
	return draft
}

func (d *desktop) bindingEditor(c *ui.Context) {
	s, draft := &d.secrets, d.secrets.draft
	status := s.slot.Status
	scope := status.Project.Slug + " / " + status.Profile + " / " + status.Task + " / " + status.Requirement
	choices := []desktopOption{{newBindingID, "创建新引用"}}
	for _, b := range s.slot.Reusable {
		choices = append(choices, desktopOption{b.ID, b.Alias + " · " + b.ProviderProfile.Provider})
	}
	inspectorPanel(c, func() {
		section(c, "共享槽位映射")
		labelValue(c, "写入项目 / 配置方案 / 任务 / 槽位", scope)
		optionPicker(c, "选择共享引用", draft.choice, choices, s.wait, func(id string) {
			draft.choice, draft.issue = id, ""
		})
		if draft.choice == newBindingID {
			bindingMetadataFields(c, draft, s.wait)
		} else {
			for _, b := range s.slot.Reusable {
				if b.ID == draft.choice {
					labelValue(c, "Provider 配置 · 认证方式", b.ProviderProfile.Name+" · "+b.ProviderProfile.Method)
					locatorFields(c, b.Locator)
					break
				}
			}
		}
		showManagementError(c, draft.issue)
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.Spacer(c)
			if ui.Button(c, "取消编辑").Disabled(s.wait).Clicked() {
				s.draft = nil
			}
			if ui.PrimaryButton(c, "预览共享变更…").Disabled(s.wait || !draft.valid()).Clicked() {
				draft.confirmed = draft.ref()
				s.confirm = true
			}
		})
	})
	if s.confirm {
		alias := draft.confirmed.Alias
		if alias == "" {
			for _, b := range s.slot.Reusable {
				if b.ID == draft.confirmed.BindingID {
					alias = b.Alias
					break
				}
			}
		}
		previous := "未绑定"
		if status.Binding != nil {
			previous = status.Binding.Alias
		}
		message := fmt.Sprintf("%s\n\n%s → %s\n\n保存的是共享引用。发布后，此方案的使用者将收到新映射。", scope, previous, alias)
		if ui.AlertDialog(c, &s.confirm, "确认共享槽位映射", message, "返回编辑", "保存共享映射") == 1 {
			d.saveMapping(draft)
		}
	}
}

func bindingMetadataFields(c *ui.Context, draft *bindingDraft, disabled bool) {
	p := &draft.change.ProviderProfile
	loc := &draft.change.Locator
	ui.Text(c, "填写 provider 的条目定位信息。").FillWidth().TextColor(muted)
	labeledInput(c, "绑定别名", &draft.change.Alias, "test-db-reader", disabled)
	optionPicker(c, "Provider", p.Provider, []desktopOption{{infisical.Name, "Infisical"}, {bitwarden.Name, "Bitwarden"}}, disabled, func(id string) {
		p.Provider, p.Method = id, "personal"
		*loc = provider.Locator{}
		if id == infisical.Name {
			loc.Path = "/"
		}
	})
	labeledInput(c, "Provider 配置", &p.Name, "default", disabled)
	methods := []desktopOption{{"personal", "personal"}}
	if p.Provider == infisical.Name {
		methods = append(methods, desktopOption{"universal-auth", "universal-auth"})
	}
	optionPicker(c, "认证方式", p.Method, methods, disabled, func(id string) { p.Method = id })
	if p.Provider == bitwarden.Name {
		labeledInput(c, "条目 UUID", &loc.EntryID, "", disabled)
		return
	}
	labeledInput(c, "后端项目 ID", &loc.ProjectID, "", disabled)
	labeledInput(c, "后端环境", &loc.Environment, "dev", disabled)
	labeledInput(c, "条目路径", &loc.Path, "/", disabled)
	labeledInput(c, "条目键名", &loc.Key, "", disabled)
}

func labeledInput(c *ui.Context, name string, value *string, hint string, disabled bool) {
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, name).FontSize(13).TextColor(muted)
		ui.TextInput(c, value).Label(name).Placeholder(hint).FillWidth().Disabled(disabled)
	})
}

func (draft bindingDraft) valid() bool {
	if draft.choice != newBindingID {
		return draft.choice != ""
	}
	change := draft.ref()
	if change.Alias == "" || change.ProviderProfile.Name == "" {
		return false
	}
	loc := change.Locator
	if change.ProviderProfile.Provider == bitwarden.Name {
		return bitwarden.ValidLocator(loc)
	}
	return loc.ProjectID != "" && loc.Environment != "" && strings.HasPrefix(loc.Path, "/") && loc.Key != ""
}

func (draft bindingDraft) ref() app.SecretBindingChange {
	if draft.choice != newBindingID {
		return app.SecretBindingChange{BindingID: draft.choice}
	}
	change := draft.change
	p, loc := &change.ProviderProfile, &change.Locator
	change.Alias, p.Name = strings.TrimSpace(change.Alias), strings.TrimSpace(p.Name)
	loc.EntryID, loc.ProjectID, loc.Environment, loc.Path, loc.Key = strings.TrimSpace(loc.EntryID), strings.TrimSpace(loc.ProjectID),
		strings.TrimSpace(loc.Environment), strings.TrimSpace(loc.Path), strings.TrimSpace(loc.Key)
	return change
}

func (d *desktop) saveMapping(draft *bindingDraft) {
	s := &d.secrets
	expected, change := s.slot, draft.confirmed
	s.wait, draft.issue = true, ""
	desktopWork(d, func(ctx context.Context) (app.DesktopSecretReceipt, error) {
		return d.core.BindDesktopSecret(ctx, expected, change)
	}, func(result app.DesktopSecretReceipt, err error) {
		s.wait, draft.issue = false, failureCode(err)
		if err != nil {
			return
		}
		d.loadSlot()
		s.scroll.Y = 0
		state := "映射已生效"
		switch result.State {
		case "local":
			state = "已保存到本机"
		case "queued":
			state = "已保存到本机 · 共享变更待发布"
		}
		s.setReceipt(fmt.Sprintf("%s。%s / %s / %s / %s → %s", state,
			expected.Status.Project.Slug, expected.Status.Profile, expected.Status.Task, expected.Status.Requirement, result.Alias))
	})
}
