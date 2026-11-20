package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

type conflictPage struct {
	id         string
	detail     app.ConflictDetail
	diffs      [3]map[string]*conflictTextDiff
	choice     string
	draft      *conflictDraft
	scroll     ui.ScrollState
	all        bool
	metadata   bool
	confirm    bool
	loading    bool
	writing    bool
	review     bool
	revision   string
	resolvedAt time.Time
	err        string
}

type conflictDraft struct {
	input         app.ResolutionInput
	summary       string
	observedAt    string
	bodyHidden    bool
	summaryHidden bool
}

func (d *desktop) writingConflict() bool {
	return d.conflict != nil && d.conflict.writing
}

func (d *desktop) openConflict(id string) {
	d.conflict = &conflictPage{id: id}
	d.loadConflict()
}

func (d *desktop) loadConflict() {
	p := d.conflict
	if p == nil || p.loading || p.writing || p.revision != "" {
		return
	}
	p.loading, p.confirm = true, false
	type comparison struct {
		detail app.ConflictDetail
		diffs  [3]map[string]*conflictTextDiff
	}
	runDesktop(d, func(ctx context.Context) (comparison, error) {
		detail, err := d.core.Conflict(ctx, p.id)
		return comparison{detail, conflictTextDiffs(detail)}, err
	}, func(result comparison, err error) {
		if d.conflict != p {
			return
		}
		p.loading = false
		if err != nil {
			p.err = conflictError(err)
			p.review = true
			return
		}
		if p.detail.ConflictID != "" && p.detail.ConflictID != result.detail.ConflictID {
			p.err = "The record changed after comparison. The latest versions are loaded; choose the resolution again."
			p.choice = ""
			if p.draft != nil {
				p.draft.input.Policy.AgentDisclosure = ""
				p.err += " Your document draft is preserved; review it and choose disclosure again."
			}
		} else {
			p.err = ""
		}
		p.detail, p.diffs, p.review = result.detail, result.diffs, false
	})
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
	if d.err != nil || d.publicationUnknown() || !p.canConfirm() {
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
	runDesktop(d, func(ctx context.Context) (app.SyncResult, error) {
		return d.core.Resolve(ctx, p.id, token, take, input)
	}, func(result app.SyncResult, err error) {
		if d.conflict != p {
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
		p.resolvedAt = time.Now()
		d.resolvedRevision = result.ResolvedRevision
		d.refresh()
	})
}

func conflictError(err error) string {
	code := app.Classify(err).Code
	switch code {
	case "stale-conflict":
		return "The record changed after comparison. Load the latest versions; your draft remains on this page."
	case "conflict-not-found":
		return "This record no longer has an active conflict. Return to the Queue for its current state."
	case "sync-busy", "store-busy":
		return "The local sync directory is busy. Retry after the current writer finishes."
	case "push-pending":
		return "A publication outcome is uncertain. Reconcile it in Repository before resolving a conflict."
	case "size-limit":
		return "The replacement exceeds the size limit. Shorten its body or metadata."
	case "incomplete", "invalid-policy", "unsupported-schema":
		return "The core cannot create a revision from this result. Review the resolution and edited fields (" + code + ")."
	default:
		return "The operation did not complete. Reread the conflict before retrying (" + code + ")."
	}
}
