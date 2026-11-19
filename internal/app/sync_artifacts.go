package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

const (
	routinePublicationBudget = 5 << 20
	historyWarningBytes      = 100 << 20
	historyPauseBytes        = 250 << 20
	artifactStagingBudget    = 320 << 20
)

type SyncHistory struct {
	RawBytes              int64  `json:"rawBytes"`
	Warning               string `json:"warning,omitempty"`
	AutomaticPayloadPause bool   `json:"automaticPayloadPause"`
}

func syncHistory(size int64) *SyncHistory {
	h := &SyncHistory{RawBytes: size, AutomaticPayloadPause: size >= historyPauseBytes}
	switch {
	case h.AutomaticPayloadPause:
		h.Warning = "Git history is about 250 MiB or more. New automatic artifact payloads are paused."
	case size >= historyWarningBytes:
		h.Warning = "Git history is about 100 MiB or more. Published plaintext remains in history."
	}
	return h
}

// Raw sizes of distinct reachable Git objects are independent of the local
// repository's packing and exclude unrelated, rejected or abandoned candidates.
func measureSyncHistory(ctx context.Context, dir, head string) (int64, error) {
	ids, err := syncGitRaw(ctx, dir, nil, "rev-list", "--objects", "--no-object-names", head, "--")
	if err != nil {
		return 0, err
	}
	sizes, err := syncGit(ctx, dir, []byte(ids), "cat-file", "--batch-check=%(objectsize)")
	if err != nil {
		return 0, err
	}
	var total int64
	for _, line := range strings.Split(sizes, "\n") {
		size, err := strconv.ParseInt(line, 10, 64)
		if err != nil || size < 0 || total+size < total {
			return 0, Fail("incomplete", "could not measure confirmed Git history")
		}
		total += size
	}
	return syncHistorySize(total), nil
}

func saveSyncHistory(ctx context.Context, c *sql.Conn, size int64) error {
	_, err := c.ExecContext(ctx, "UPDATE sync_state SET history_bytes=?", size)
	return err
}

// Apply the payload budget only after the record planner has selected a closed
// set. Metadata remains part of that set, even when the payload cannot travel.
func (a App) stageArtifactPublications(files, base map[string][]byte, selected []operation, history int64) error {
	var added, staged int64
	for path, data := range files {
		if old, exists := base[path]; !exists || !bytes.Equal(old, data) {
			added += int64(len(data))
		}
		if strings.HasPrefix(path, "objects/") {
			staged += int64(len(data))
		}
	}
	var artifacts []remoteRecord
	for _, o := range selected {
		r, err := decodeRecord(files[recordPath(o.id)])
		if err != nil {
			return err
		}
		if r.Kind == artifactKind && !r.Deleted && r.Artifact.Omitted == "" {
			artifacts = append(artifacts, r)
		}
	}
	// Review-approved samples get first use of the bytes left after ordinary
	// catalog changes. Preserve the stable order within each mode.
	sort.SliceStable(artifacts, func(i, j int) bool {
		return artifacts[i].Artifact.Mode == "explicit" && artifacts[j].Artifact.Mode != "explicit"
	})
	for _, r := range artifacts {
		p := r.Artifact
		object := p.objectPath()
		if data, ok := files[object]; ok && len(data) == int(p.Size) && !excludedArtifactBytes(data) {
			continue
		}
		// The explicit per-file limit is raw bytes. Allow the corresponding
		// record/envelope overhead even for a sample of exactly 5 MiB.
		budget := int64(routinePublicationBudget)
		if p.Mode == "explicit" {
			budget += MetadataLimit
		}
		switch {
		case p.Mode == "automatic" && (history >= historyPauseBytes || history+added+p.Size > historyPauseBytes):
			p.Omitted = "history-pause"
		case staged+p.Size > artifactStagingBudget:
			p.Omitted = "staging-budget"
		case added+p.Size > budget:
			p.Omitted = "publication-budget"
		default:
			data, err := a.readLocalArtifactBytes(p)
			switch {
			case err != nil:
				p.Omitted = "payload-unavailable"
			case excludedArtifactBytes(data):
				p.Omitted = "excluded-format"
			default:
				files[object] = data
				added += p.Size
				staged += p.Size
				continue
			}
		}
		r.Payload = p
		data, err := json.Marshal(r.publishedEnvelope)
		if err != nil {
			return err
		}
		files[recordPath(r.ID)] = data
	}
	return nil
}

func (a App) stageInitialArtifacts(files map[string][]byte) error {
	var selected []operation
	for path, b := range files {
		if strings.HasPrefix(path, "records/") {
			r, err := decodeRecord(b)
			if err != nil {
				return err
			}
			if r.Kind == artifactKind {
				selected = append(selected, operation{id: r.ID})
			}
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].id < selected[j].id })
	return a.stageArtifactPublications(files, nil, selected, 0)
}

// A context-only replacement can validate against confirmed artifacts without
// treating pending artifact intents as already published.
func addConfirmedArtifactObjects(files, base map[string][]byte) error {
	for path, b := range files {
		if !strings.HasPrefix(path, "records/") {
			continue
		}
		r, err := decodeRecord(b)
		if err != nil {
			return err
		}
		if r.Kind != artifactKind {
			continue
		}
		prior, ok := base[path]
		if !ok {
			delete(files, path)
			continue
		}
		files[path] = prior
		r, err = decodeRecord(prior)
		if err != nil {
			return err
		}
		if r.Artifact.Omitted == "" {
			path := r.Artifact.objectPath()
			files[path] = base[path]
		}
	}
	return nil
}

const publicationBudgetReason = "publication-budget"

// Budget complete dependency units. A single indivisible unit can exceed the
// target, so large related edits can still make progress.
func limitPublicationUnits(remote, local map[string][]byte, ops []operation, edges map[string][]string, blocked map[string]string) error {
	eligible := map[string]operation{}
	objects := map[string][]string{}
	owners := map[string]string{}
	for _, o := range ops {
		if blocked[o.id] != "" {
			continue
		}
		eligible[o.id] = o
		for _, b := range [][]byte{o.proposed, o.base} {
			if len(b) == 0 {
				continue
			}
			r, err := decodeRecord(b)
			if err != nil {
				return err
			}
			if bytes.Equal(b, o.proposed) {
				objects[o.id] = r.ordinaryObjectPaths()
			}
			key := ""
			switch {
			case r.Kind == "project":
				key = "slug:" + r.Value.Slug
			case r.Kind == "context" && r.Value.Purpose == "project-summary":
				key = "summary:" + r.Scope.ID
			}
			if key == "" {
				continue
			}
			if other := owners[key]; other != "" && other != o.id {
				edges[o.id] = append(edges[o.id], other)
				edges[other] = append(edges[other], o.id)
			}
			owners[key] = o.id
		}
	}
	seen := map[string]bool{}
	reused := map[string]bool{}
	var added int
	for _, o := range ops {
		if _, ok := eligible[o.id]; !ok || seen[o.id] {
			continue
		}
		group := []string{o.id}
		seen[o.id] = true
		paths := map[string]bool{}
		var size int
		for i := 0; i < len(group); i++ {
			id := group[i]
			pending := eligible[id]
			if !bytes.Equal(remote[recordPath(id)], pending.proposed) {
				size += len(pending.proposed)
			}
			for _, path := range objects[id] {
				if _, exists := remote[path]; !exists && !reused[path] && !paths[path] {
					size += len(local[path])
					paths[path] = true
				}
			}
			for _, dependency := range edges[id] {
				if !seen[dependency] {
					seen[dependency] = true
					group = append(group, dependency)
				}
			}
		}
		if added > 0 && added+size > routinePublicationBudget {
			for _, id := range group {
				blocked[id] = publicationBudgetReason
			}
			continue
		}
		added += size
		for path := range paths {
			reused[path] = true
		}
	}
	return nil
}

func pruneLargeEvidence(files map[string][]byte) error {
	used := map[string]bool{}
	for path, b := range files {
		if !strings.HasPrefix(path, "records/") {
			continue
		}
		r, err := decodeRecord(b)
		if err != nil {
			return err
		}
		if r.Kind == artifactKind && r.Artifact.Omitted == "" {
			used[r.Artifact.objectPath()] = true
		}
	}
	for path, data := range files {
		if strings.HasPrefix(path, "objects/") && len(data) > BodyLimit && !used[path] {
			delete(files, path)
		}
	}
	return nil
}
