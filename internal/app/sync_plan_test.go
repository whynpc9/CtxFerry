package app

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func planRecord(id, kind, parent, revision string) []byte {
	r := publishedEnvelope{SchemaVersion: 1, ID: id, Kind: kind, Revision: revision, Scope: Scope{"workspace", "workspace"}, Payload: map[string]any{}}
	if parent != "" {
		r.Scope = Scope{"project", parent}
	}
	if kind == "project" {
		r.Payload = map[string]any{"id": id, "slug": id, "createdAt": "stamp"}
	}
	b, _ := json.Marshal(r)
	return b
}
func TestPlanHoldsPendingDependenciesAndKeepsIndependentBodiesPrivate(t *testing.T) {
	parent, child, other, stable := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := planRecord(parent, "project", "", "base")
	remote := map[string][]byte{recordPath(parent): planRecord(parent, "project", "", "remote"), recordPath(stable): planRecord(stable, "project", "", "stable")}
	proposed := planRecord(parent, "project", "", "local")
	childBytes := planRecord(child, "context", parent, "child")
	otherBytes := planRecord(other, "context", stable, "other")
	// Attach an object only to the held child.
	var payload map[string]any
	json.Unmarshal(childBytes, &payload)
	payload["payload"] = map[string]any{"body": objectManifest{"text/markdown", 7, "private"}}
	childBytes, _ = json.Marshal(payload)
	merged, selected, conflicts, blocked, e := planOperations(remote, map[string][]byte{"objects/private": []byte("private")}, []operation{{parent, base, proposed, "p"}, {child, nil, childBytes, "c"}, {other, nil, otherBytes, "o"}})
	if e != nil || !conflicts[parent] || blocked[child] == "" || len(selected) != 1 || selected[0].id != other {
		t.Fatal(e, selected, conflicts, blocked)
	}
	if merged[recordPath(child)] != nil || merged["objects/private"] != nil {
		t.Fatal("blocked content was published")
	}
}
func TestUnchangedParentDoesNotCoupleDocuments(t *testing.T) {
	parent, a, b := uuid.NewString(), uuid.NewString(), uuid.NewString()
	old := planRecord(a, "context", parent, "old")
	remote := map[string][]byte{recordPath(parent): planRecord(parent, "project", "", "p"), recordPath(a): planRecord(a, "context", parent, "remote")}
	_, selected, _, blocked, e := planOperations(remote, nil, []operation{{a, old, planRecord(a, "context", parent, "local"), "a"}, {b, nil, planRecord(b, "context", parent, "b"), "b"}})
	if e != nil || len(selected) != 1 || selected[0].id != b || blocked[b] != "" {
		t.Fatal(e, selected, blocked)
	}
}
