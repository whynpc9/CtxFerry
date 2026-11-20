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
	detailErr error
}

func (p *projectPage) loadRelated() {
	tab := p.tab
	if tab == projectConfig || p.busy || (p.related.loaded && p.related.loadedTab == tab) {
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
		ui.Text(c, "正在读取项目记录…").TextColor(muted)
		return
	}
	switch p.tab {
	case projectContext:
		p.contextView(c)
	case projectResources:
		section(c, "项目资源")
		projectEmpty(c, len(r.resources), "此项目尚无资源")
		for _, resource := range r.resources {
			projectRecord(c, resource.Identity, resource.Kind+" · "+resource.Stage+" · "+resource.Purpose)
		}
	case projectTasks:
		p.tasksView(c)
	case projectSkills:
		section(c, "项目 Skills")
		projectEmpty(c, len(r.skills), "此项目尚未设置共享 Skill 选择")
		for _, name := range r.skills {
			projectRecord(c, name, "共享选择")
		}
	case projectArtifacts:
		section(c, "项目产物")
		projectEmpty(c, len(r.artifacts), "此项目尚无产物")
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
	ui.Column(c).FillWidth().Padding(10, 12).Gap(4).Background(panel).Radius(5).Children(func() {
		ui.Text(c, name).Bold().Multiline(true)
		if description != "" {
			ui.Text(c, description).TextColor(muted).Multiline(true)
		}
	})
}

func projectEmpty(c *ui.Context, count int, message string) {
	if count == 0 {
		ui.Text(c, message).TextColor(muted)
	}
}

func (p *projectPage) contextView(c *ui.Context) {
	r := &p.related
	if doc := r.document; doc != nil {
		if ui.Button(c, "返回 Context 列表").Clicked() {
			r.document = nil
			return
		}
		section(c, doc.Title)
		detail(c, "用途", doc.Purpose)
		detail(c, "来源", doc.Source)
		detail(c, "修订", doc.Revision)
		detail(c, "更新", doc.UpdatedAt)
		detail(c, "新鲜度", doc.Freshness)
		detail(c, "披露", doc.Policy.Storage+" · "+doc.Policy.AgentDisclosure)
		switch {
		case doc.Body != nil:
			ui.Text(c, *doc.Body).FillWidth().Multiline(true).Selectable()
		case doc.Summary != nil:
			ui.Text(c, *doc.Summary).FillWidth().Multiline(true).Selectable()
		default:
			ui.Text(c, "当前披露规则仅提供文档元数据。").Multiline(true)
		}
		return
	}
	section(c, "Context")
	projectEmpty(c, len(r.context.Documents), "此项目尚无可见 Context 文档")
	for _, doc := range r.context.Documents {
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			if ui.Button(c, doc.Title).Disabled(p.busy).Clicked() {
				p.openContext(doc.ID)
			}
			ui.Text(c, doc.Purpose).TextColor(muted).Grow(1).Multiline(true)
		})
	}
	if r.context.Truncated {
		if ui.Button(c, "读取更多 Context").Disabled(p.busy).Clicked() {
			p.readMoreContext(r.context.NextCursor)
		}
	}
}

func (p *projectPage) openContext(id string) {
	project, cwd := p.target.ProjectID, p.core.DataDir
	p.work(func() func() {
		result, err := p.core.QueryContext(p.ctx, project, cwd, "get", id, "")
		return func() { p.related.document, p.related.detailErr = result.Document, err }
	})
}

func (p *projectPage) readMoreContext(cursor string) {
	project, cwd := p.target.ProjectID, p.core.DataDir
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
		if ui.Button(c, "返回 Tasks 列表").Disabled(p.busy).Clicked() {
			r.task, r.review, r.detailErr = nil, nil, nil
			return
		}
		section(c, task.Name)
		ui.Text(c, task.Purpose).Multiline(true)
		if len(task.Inputs) > 0 {
			detail(c, "输入", strings.Join(task.Inputs, " · "))
		}
		for _, slot := range task.ResourceSlots {
			detail(c, "资源槽位", slot.Name+" · "+slot.Kind+" · "+slot.Stage)
		}
		for _, requirement := range task.CredentialRequirements {
			detail(c, "凭据槽位", requirement.Name+" · "+requirement.Type)
		}
		if p.target.CheckoutID == "" || p.config.CheckoutProblem != "" {
			ui.Text(c, "选择可用的本机 Checkout 后可核对执行定义。").Multiline(true)
		} else if ui.Button(c, "核对本机执行定义").Disabled(p.busy).Clicked() {
			p.reviewTask(task.ID)
		}
		if r.review != nil {
			detail(c, "本机执行状态", executionState(r.review.State))
			for _, change := range r.review.Changes {
				detail(c, "已变更字段", change.Field)
			}
		}
		return
	}
	section(c, "Tasks")
	projectEmpty(c, len(r.tasks), "此项目尚无 Task")
	for i := range r.tasks {
		task := &r.tasks[i]
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			if ui.Button(c, task.Name).Clicked() {
				r.task = task
			}
			ui.Text(c, task.Purpose).TextColor(muted).Grow(1).Multiline(true)
		})
	}
}

func executionState(state string) string {
	switch state {
	case "not-required":
		return "无需凭据执行启用"
	case "not-enabled":
		return "此设备尚未启用"
	case "enabled":
		return "当前定义已在此设备启用"
	case "execution-definition-changed":
		return "定义有变更，需重新核对后启用"
	default:
		return state
	}
}

func (p *projectPage) reviewTask(id string) {
	project, path := p.target.ProjectID, p.checkoutChoice
	p.work(func() func() {
		review, err := p.core.PreviewExecution(p.ctx, project, path, id, path, "")
		return func() {
			p.related.detailErr = err
			p.related.review = nil
			if err == nil {
				p.related.review = &review
			}
		}
	})
}
