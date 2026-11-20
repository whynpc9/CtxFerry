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
	choices := []desktopOption{{newBindingID, "Create a new reference"}}
	for _, b := range s.slot.Reusable {
		choices = append(choices, desktopOption{b.ID, b.Alias + " · " + b.ProviderProfile.Provider})
	}
	card(c).Children(func() {
		cardHeader(c, icLink, "Shared requirement mapping", "Reuse a compatible reference or enter locator metadata. Secret values are never placed in drafts or commands.", nil)
		labelValue(c, "Project / Profile / Task / requirement", scope)
		optionPicker(c, "Shared reference", draft.choice, choices, s.wait, func(id string) {
			draft.choice, draft.issue = id, ""
		})
		if draft.choice == newBindingID {
			bindingMetadataFields(c, draft, s.wait)
		} else {
			for _, b := range s.slot.Reusable {
				if b.ID == draft.choice {
					labelValue(c, "Provider profile · method", b.ProviderProfile.Name+" · "+b.ProviderProfile.Method)
					locatorFields(c, b.Locator)
					break
				}
			}
		}
		showManagementError(c, draft.issue)
		ui.Row(c).FillWidth().Gap(sp2).Children(func() {
			ui.Spacer(c)
			ui.Button(c, "Cancel").Disabled(s.wait).OnClick(func() { s.draft = nil })
			ui.PrimaryButton(c, "Preview shared change…").Disabled(s.wait || !draft.valid()).OnClick(func() {
				draft.confirmed = draft.ref()
				s.confirm = true
			})
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
		previous := "Not mapped"
		if status.Binding != nil {
			previous = status.Binding.Alias
		}
		message := fmt.Sprintf("%s\n\n%s → %s\n\nThis saves a shared reference mapping. Once published, other devices using this Profile receive the new mapping. Provider-held values are not copied.", scope, previous, alias)
		if ui.AlertDialog(c, &s.confirm, "Confirm shared requirement mapping", message, "Back to edit", "Save shared mapping") == 1 {
			d.saveMapping(draft)
		}
	}
}

func bindingMetadataFields(c *ui.Context, draft *bindingDraft, disabled bool) {
	p := &draft.change.ProviderProfile
	loc := &draft.change.Locator
	muted(c, "Enter provider entry locator metadata, not its value.")
	labeledInput(c, "Binding alias", &draft.change.Alias, "test-db-reader", disabled)
	optionPicker(c, "Provider", p.Provider, []desktopOption{{infisical.Name, "Infisical"}, {bitwarden.Name, "Bitwarden"}}, disabled, func(id string) {
		p.Provider, p.Method = id, "personal"
		*loc = provider.Locator{}
		if id == infisical.Name {
			loc.Path = "/"
		}
	})
	labeledInput(c, "Provider profile", &p.Name, "default", disabled)
	methods := []desktopOption{{"personal", "personal"}}
	if p.Provider == infisical.Name {
		methods = append(methods, desktopOption{"universal-auth", "universal-auth"})
	}
	optionPicker(c, "Authentication method", p.Method, methods, disabled, func(id string) { p.Method = id })
	if p.Provider == bitwarden.Name {
		labeledInput(c, "Entry UUID", &loc.EntryID, "", disabled)
		return
	}
	labeledInput(c, "Backend Project ID", &loc.ProjectID, "", disabled)
	labeledInput(c, "Backend environment", &loc.Environment, "dev", disabled)
	labeledInput(c, "Entry path", &loc.Path, "/", disabled)
	labeledInput(c, "Entry key", &loc.Key, "", disabled)
}

func labeledInput(c *ui.Context, name string, value *string, hint string, disabled bool) {
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, name).FontSize(fsMeta).TextColor(tk.Muted)
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
		state := "Mapping is effective"
		switch result.State {
		case "local":
			state = "Saved on this device"
		case "queued":
			state = "Saved locally · shared change queued for publication"
		}
		s.setReceipt(fmt.Sprintf("%s. %s / %s / %s / %s → %s", state,
			expected.Status.Project.Slug, expected.Status.Profile, expected.Status.Task, expected.Status.Requirement, result.Alias))
	})
}
