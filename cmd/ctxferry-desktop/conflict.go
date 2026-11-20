package main

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

type conflictPage struct {
	id       string
	detail   app.ConflictDetail
	diffs    [3]map[string]*conflictTextDiff
	choice   string
	draft    *conflictDraft
	scroll   ui.ScrollState
	all      bool
	metadata bool
	confirm  bool
	loading  bool
	writing  bool
	review   bool
	revision string
	err      string
}

type conflictDraft struct {
	input         app.ResolutionInput
	summary       string
	observedAt    string
	bodyHidden    bool
	summaryHidden bool
}

func (d *desktop) openConflict(id string) {
	d.conflict = &conflictPage{id: id}
	d.loadConflict()
}

func (d *desktop) loadConflict() {
	p := d.conflict
	if p.loading || p.writing || p.revision != "" {
		return
	}
	p.loading, p.confirm = true, false
	go func() {
		detail, err := d.core.Conflict(d.ctx, p.id)
		diffs := conflictTextDiffs(detail)
		d.update(func() {
			if d.ctx.Err() != nil || d.conflict != p {
				return
			}
			p.loading = false
			if err != nil {
				p.err = conflictError(err)
				p.review = true
				return
			}
			if p.detail.ConflictID != "" && p.detail.ConflictID != detail.ConflictID {
				p.err = "比较后记录已变化。已载入最新版本，请重新选择解决方式。"
				p.choice = ""
				if p.draft != nil {
					p.draft.input.Policy.AgentDisclosure = ""
					p.err += "正文草稿已保留，请重新核对草稿及披露范围。"
				}
			} else {
				p.err = ""
			}
			p.detail, p.review = detail, false
			p.diffs = diffs
		})
	}()
}

func (p *conflictPage) version(choice string) *app.ConflictVersion {
	switch choice {
	case "local":
		return p.detail.Local
	case "remote":
		return p.detail.Remote
	default:
		return nil
	}
}

func (p *conflictPage) editSource() *app.ConflictVersion {
	for _, v := range []*app.ConflictVersion{p.detail.Local, p.detail.Remote} {
		if v != nil && v.Kind == "context" && !v.Deleted {
			return v
		}
	}
	return nil
}

func payload(v *app.ConflictVersion) map[string]any {
	fields, _ := v.Payload.(map[string]any)
	return fields
}

func (p *conflictPage) startDraft() {
	v := p.editSource()
	if v == nil {
		return
	}
	fields := payload(v)
	draft := &conflictDraft{}
	data, _ := json.Marshal(fields)
	// The core projects these fields from the validated Context envelope.
	_ = json.Unmarshal(data, &draft.input)
	draft.input.Policy = v.Policy
	if p.detail.Local != nil && p.detail.Remote != nil && p.detail.Local.Policy != p.detail.Remote.Policy {
		draft.input.Policy.AgentDisclosure = ""
	}
	if draft.input.Summary != nil {
		draft.summary = *draft.input.Summary
	}
	if draft.input.ObservedAt != nil {
		draft.observedAt = *draft.input.ObservedAt
	}
	draft.bodyHidden = v.Policy.AgentDisclosure != "full"
	draft.summaryHidden = v.Policy.AgentDisclosure == "reference" || v.Policy.AgentDisclosure == "local-tool"
	p.draft = draft
}

func (p *conflictPage) canConfirm() bool {
	if p.loading || p.writing || p.review || p.revision != "" || p.detail.ConflictID == "" {
		return false
	}
	if p.version(p.choice) != nil {
		return true
	}
	return p.choice == "edit" && p.editSource() != nil && p.draft != nil &&
		p.draft.input.Policy.AgentDisclosure != "" &&
		strings.TrimSpace(p.draft.input.Title) != "" &&
		strings.TrimSpace(p.draft.input.Purpose) != "" &&
		strings.TrimSpace(p.draft.input.Source) != ""
}

func (draft *conflictDraft) replacement() ([]byte, error) {
	input := draft.input
	if input.Summary != nil || draft.summary != "" {
		input.Summary = &draft.summary
	}
	if draft.observedAt == "" {
		input.ObservedAt = nil
	} else {
		input.ObservedAt = &draft.observedAt
	}
	data, err := json.Marshal(input)
	if err == nil && (len(input.Body) > app.BodyLimit || len(data) > app.BodyLimit+app.MetadataLimit) {
		err = app.Fail("size-limit", "resolution exceeds limit")
	}
	return data, err
}

func (d *desktop) resolveConflict(p *conflictPage) {
	if !p.canConfirm() {
		return
	}
	take := p.choice
	var input []byte
	if take == "edit" {
		take = ""
		var err error
		input, err = p.draft.replacement()
		if err != nil {
			p.err = conflictError(err)
			return
		}
	}
	token := p.detail.ConflictID
	p.writing, p.err = true, ""
	go func() {
		result, err := d.core.Resolve(d.ctx, p.id, token, take, input)
		d.update(func() {
			if d.ctx.Err() != nil {
				return
			}
			p.writing = false
			if err != nil {
				p.err = conflictError(err)
				switch app.Classify(err).Code {
				case "stale-conflict", "conflict-not-found":
					p.review = true
				}
				return
			}
			p.revision = result.ResolvedRevision
			d.resolvedRevision = result.ResolvedRevision
			d.refresh()
		})
	}()
}

func conflictError(err error) string {
	code := app.Classify(err).Code
	switch code {
	case "stale-conflict":
		return "比较后记录已变化，请载入最新版本。草稿仍保留在此页。"
	case "conflict-not-found":
		return "这条记录已无活动冲突。请返回同步队列查看当前状态。"
	case "sync-busy", "store-busy":
		return "本机同步目录正在使用中，请稍后重试。"
	case "push-pending":
		return "上次发布仍待确认。同步完成后可重试解决冲突。"
	case "size-limit":
		return "编辑内容超过大小上限，请缩短正文或其他字段。"
	case "incomplete", "invalid-policy", "unsupported-schema":
		return "核心无法使用此结果生成修订。请核对解决方式及编辑字段（" + code + "）。"
	default:
		return "操作未完成，请重试（" + code + "）。"
	}
}
