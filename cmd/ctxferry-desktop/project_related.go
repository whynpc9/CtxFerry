package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

type projectRelated struct {
	loadedTab int
	loaded    bool
	err       error
	context   app.ContextResult
	resources []app.Resource
	tasks     []app.Task
	skills    []string
	artifacts []app.ArtifactManifest
	document  *app.ContextDoc
	task      *app.Task
	review    *app.ExecutionReview
	evidence  []app.DesktopTaskResource
	detailErr error
	trustErr  error
}

func (p *projectPage) loadRelated() {
	tab := p.tab
	if tab == projectConfig || tab == projectOverview || p.busy || (p.related.loaded && p.related.loadedTab == tab) {
		return
	}
	p.related = projectRelated{}
	project, cwd := p.target.ProjectID, p.core.DataDir
	if p.target.CheckoutID != "" && p.config.CheckoutProblem == "" {
		cwd = p.checkoutChoice
	}
	p.work(func() func() {
		data := projectRelated{loaded: true, loadedTab: tab}
		switch tab {
		case projectContext:
			data.context, data.err = p.core.QueryContext(p.ctx, project, cwd, "show", "", "")
		case projectResources:
			result, err := p.core.ListResources(p.ctx, project, cwd)
			data.resources, data.err = result.Resources, err
		case projectTasks:
			result, err := p.core.ListTasks(p.ctx, project, cwd)
			data.tasks, data.err = result.Tasks, err
		case projectSkills:
			result, err := p.core.ReadProjectSkills(p.ctx, project)
			data.skills, data.err = result.Skills, err
		case projectArtifacts:
			result, err := p.core.ListArtifacts(p.ctx, project, cwd, "")
			data.artifacts, data.err = result.Artifacts, err
		}
		return func() { p.related = data }
	})
}

func (p *projectPage) relatedView(c *ui.Context) {
	r := &p.related
	if r.err != nil {
		projectError(c, r.err)
		return
	}
	if r.detailErr != nil {
		projectError(c, r.detailErr)
	}
	if !r.loaded {
		muted(c, "Reading Project records…")
		return
	}
	switch p.tab {
	case projectContext:
		p.contextView(c)
	case projectResources:
		section(c, "Project Resources")
		projectEmpty(c, len(r.resources), "This Project has no Resources yet.")
		for _, resource := range r.resources {
			resource := resource
			card(c).Children(func() {
				cardHeader(c, icServer, resource.Identity, resource.Kind+" · "+resource.Stage+" · "+resource.Purpose, nil)
				if p.host != nil {
					linkButton(c, "Open Resource "+resource.Identity, icArrowR).AlignSelf(ui.Start).OnClick(func() {
						p.host.navigate(desktopRoute{Page: pageFleet, ProjectID: p.target.ProjectID, RecordID: resource.ID})
					})
				}
			})
		}
	case projectTasks:
		p.tasksView(c)
	case projectSkills:
		section(c, "Project Skills")
		projectEmpty(c, len(r.skills), "This Project has no shared Skill selection.")
		for _, name := range r.skills {
			projectRecord(c, name, "Shared selection · open Skills to inspect the local target")
		}
	case projectArtifacts:
		section(c, "Project artifacts")
		projectEmpty(c, len(r.artifacts), "This Project has no registered run artifacts.")
		for _, artifact := range r.artifacts {
			state := artifact.Payload.Storage
			if artifact.Sync != nil {
				state = artifact.Sync.State
			}
			projectRecord(c, artifact.Payload.Name, fmt.Sprintf("%s · %d B · %s", artifact.Payload.Type, artifact.Payload.Size, state))
		}
	}
}

func projectRecord(c *ui.Context, name, description string) {
	card(c).Children(func() {
		strong(c, name)
		if description != "" {
			muted(c, description)
		}
	})
}

func projectEmpty(c *ui.Context, count int, message string) {
	if count == 0 {
		muted(c, message)
	}
}

func (p *projectPage) contextView(c *ui.Context) {
	r := &p.related
	if doc := r.document; doc != nil {
		card(c).Children(func() {
			cardHeader(c, icFile, doc.Title, doc.Purpose, func() {
				ui.Button(c, "Back to Context").OnClick(func() { r.document = nil })
			})
			kvGrid(c, []kvRow{
				{k: "Document ID", v: doc.ID, mono: true}, {k: "Source", v: doc.Source}, {k: "Revision", v: doc.Revision, mono: true},
				{k: "Updated", v: doc.UpdatedAt}, {k: "Freshness", v: doc.Freshness}, {k: "Disclosure", v: doc.Policy.Storage + " · " + doc.Policy.AgentDisclosure},
			})
			ui.Divider(c)
			switch {
			case doc.Body != nil:
				ui.Text(c, *doc.Body).FillWidth().Multiline(true).Selectable().LineHeight(1.5)
			case doc.Summary != nil:
				banner(c, toneUnknown, "Content is limited by disclosure", "Only the document summary is available under the current policy.", nil)
				ui.Text(c, *doc.Summary).FillWidth().Multiline(true).Selectable()
			default:
				banner(c, toneUnknown, "Content is unavailable", "The current disclosure rule provides metadata only; this is different from an empty document.", nil)
			}
		})
		return
	}
	card(c).Children(func() {
		cardHeader(c, icFile, "Context documents", "The Checkout and resolved Profile stay fixed for listing, opening and pagination.", nil)
		projectEmpty(c, len(r.context.Documents), "No visible Context documents in this scope.")
		ui.Column(c).Dividers(1, tk.Border).Children(func() {
			for _, doc := range r.context.Documents {
				doc := doc
				ui.Row(c).Gap(sp3).Padding(sp2, 0).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						strong(c, doc.Title)
						muted(c, doc.Purpose)
					})
					ui.Button(c, "Open document").Label("Open Context " + doc.Title).Disabled(p.busy).OnClick(func() { p.openContext(doc.ID) })
				})
			}
		})
		if r.context.Truncated {
			listFooter(c, "More documents are available under the same scope", func() {
				ui.Button(c, "Load more").Disabled(p.busy).OnClick(func() { p.readMoreContext(r.context.NextCursor) })
			})
		}
	})
}

func (p *projectPage) selectedCheckoutDir() string {
	if p.target.CheckoutID != "" && p.config.CheckoutProblem == "" {
		return p.checkoutChoice
	}
	return p.core.DataDir
}

func (p *projectPage) openContext(id string) {
	project, cwd := p.target.ProjectID, p.selectedCheckoutDir()
	p.work(func() func() {
		result, err := p.core.QueryContext(p.ctx, project, cwd, "get", id, "")
		return func() { p.related.document, p.related.detailErr = result.Document, err }
	})
}

func (p *projectPage) readMoreContext(cursor string) {
	project, cwd := p.target.ProjectID, p.selectedCheckoutDir()
	p.work(func() func() {
		result, err := p.core.QueryContext(p.ctx, project, cwd, "show", "", cursor)
		return func() {
			p.related.detailErr = err
			if err == nil {
				r := &p.related.context
				r.Documents = append(r.Documents, result.Documents...)
				r.Truncated, r.NextCursor = result.Truncated, result.NextCursor
			}
		}
	})
}

func (p *projectPage) tasksView(c *ui.Context) {
	r := &p.related
	if task := r.task; task != nil {
		p.taskDetail(c, *task)
		return
	}
	card(c).Children(func() {
		cardHeader(c, icTasks, "Tasks", "Open a Task to recheck trust and resolve its slots through the saved Profile.", nil)
		projectEmpty(c, len(r.tasks), "This Project has no Tasks.")
		ui.Column(c).Dividers(1, tk.Border).Children(func() {
			for i := range r.tasks {
				task := &r.tasks[i]
				ui.Row(c).Gap(sp3).Padding(sp2, 0).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						strong(c, task.Name)
						muted(c, task.Purpose)
					})
					ui.Button(c, "Open Task").Label("Open Task " + task.Name).OnClick(func() {
						r.task = task
						if p.target.CheckoutID != "" && p.config.CheckoutProblem == "" {
							p.reviewTask(task.ID)
						}
					})
				})
			}
		})
	})
}

func (p *projectPage) taskDetail(c *ui.Context, task app.Task) {
	r := &p.related
	pageHeader(c, task.Name, task.Purpose, func() {
		ui.Button(c, "Back to Tasks").Disabled(p.busy).OnClick(func() {
			r.task, r.review, r.trustErr, r.evidence = nil, nil, nil, nil
		})
	})
	if len(task.Inputs) > 0 {
		detail(c, "Inputs", strings.Join(task.Inputs, " · "))
	}
	card(c).Children(func() {
		cardHeader(c, icServer, "Resource slots", "Resolved through the selected Profile. Probe evidence is separate from authentication and execution trust.", nil)
		for _, slot := range task.ResourceSlots {
			var evidence *app.DesktopTaskResource
			for i := range r.evidence {
				if r.evidence[i].Slot == slot.Name {
					evidence = &r.evidence[i]
				}
			}
			ui.Column(c).Gap(sp2).Padding(sp2, 0).BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Children(func() {
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
					strong(c, slot.Name)
					tag(c, slot.Kind+" · "+slot.Stage)
				})
				if evidence == nil {
					muted(c, "No mapped Resource evidence is available. Choose a Profile with this slot mapped, then Refresh.")
					return
				}
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
					body(c, "Resource")
					strong(c, evidence.Resource.Identity)
					idChip(c, evidence.Resource.ID)
				})
				if evidence.Probe != nil {
					detail(c, probeTargetLabel(evidence.Probe.Scope), evidence.Probe.Scope.Target)
					if latest := evidence.Latest; latest != nil {
						state := observationLabel(latest)
						if latest.Scope != evidence.Probe.Scope {
							state += " · previous route"
						}
						detail(c, "Latest local probe", state+" · "+timeLabel(latest.ObservedAt))
					} else {
						muted(c, "No local probe observation for this exact target.")
					}
				} else {
					muted(c, "No applicable local TCP probe route; evidence remains unknown.")
				}
				if p.host != nil {
					linkButton(c, "Open Resource "+evidence.Resource.Identity, icArrowR).AlignSelf(ui.Start).OnClick(func() {
						p.host.navigate(desktopRoute{Page: pageFleet, ProjectID: p.target.ProjectID, RecordID: evidence.Resource.ID})
					})
				}
			})
		}
	})
	card(c).Children(func() {
		cardHeader(c, icKey, "Credential requirements", "Each requirement keeps its Task, Profile and resource-slot scope.", nil)
		for _, requirement := range task.CredentialRequirements {
			requirement := requirement
			ui.Row(c).Gap(sp3).Padding(sp2, 0).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					strong(c, requirement.Name)
					muted(c, requirement.Purpose+" · "+requirement.Type+" · slot "+requirement.ResourceSlot)
				})
				if p.host != nil {
					linkButton(c, "Open requirement", icArrowR).OnClick(func() {
						profile := ""
						if p.config.Profile != nil {
							profile = p.config.Profile.Name
						}
						p.host.openCredential(p.target.ProjectID, profile, task.ID, requirement.Name)
					})
				}
			})
		}
	})
	p.taskTrustView(c, task)
}

func (p *projectPage) taskTrustView(c *ui.Context, task app.Task) {
	r := &p.related
	card(c).Children(func() {
		cardHeader(c, icShield, "Execution trust on "+p.device, "Trust is local to this Checkout and never exported in Backup.", nil)
		switch {
		case p.target.CheckoutID == "" || p.config.CheckoutProblem != "":
			muted(c, "Unknown. Choose an available local Checkout first.")
			return
		case r.trustErr != nil:
			muted(c, "Unknown; the current execution definition could not be checked.")
			projectError(c, r.trustErr)
		case p.busy:
			muted(c, "Checking the local execution definition…")
		case r.review != nil:
			strong(c, executionState(r.review.State))
			for _, change := range r.review.Changes {
				detail(c, "Changed field", change.Field)
			}
		}
		ui.Row(c).Gap(sp2).Wrap().Children(func() {
			ui.Button(c, "Recheck execution definition").AlignSelf(ui.Start).Disabled(p.busy).OnClick(func() { p.reviewTask(task.ID) })
			if p.host != nil {
				profile := ""
				if p.config.Profile != nil {
					profile = p.config.Profile.Name
				}
				p.host.commandBlock(c, desktopCommand(p.host.core.DataDir, "task", "trust", "preview", task.ID, "--profile", profile, "--checkout", p.checkoutChoice), false)
			}
		})
	})
}

func executionState(state string) string {
	switch state {
	case "not-required":
		return "No credential execution enablement required"
	case "not-enabled":
		return "Not trusted on this Checkout"
	case "enabled":
		return "Trusted · current definition enabled"
	case "execution-definition-changed":
		return "Recheck · the execution definition changed"
	default:
		return state
	}
}

func (p *projectPage) reviewTask(id string) {
	project, path := p.target.ProjectID, p.checkoutChoice
	profile := ""
	if p.config.Profile != nil {
		profile = p.config.Profile.Name
	}
	p.work(func() func() {
		review, trustErr := p.core.PreviewExecution(p.ctx, project, path, id, path, "")
		evidence, _ := p.core.DesktopTaskResources(p.ctx, project, profile, id)
		return func() {
			p.related.trustErr = trustErr
			p.related.review = nil
			if trustErr == nil {
				p.related.review = &review
			}
			p.related.evidence = evidence
		}
	})
}
