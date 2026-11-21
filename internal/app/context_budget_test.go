package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func jsonSize(r ContextResult) (int, error) {
	b, err := json.Marshal(r)
	return len(b), err
}

func pageOf(ids ...string) ContextResult {
	r := ContextResult{Documents: []ContextDoc{}}
	for _, id := range ids {
		r.Documents = append(r.Documents, ContextDoc{ID: id, Title: strings.Repeat("t", 200)})
	}
	return r
}

func TestFitContextPageDropsHighestEntriesAndPointsCursorAtLastKept(t *testing.T) {
	full := pageOf("a", "b", "c", "d", "e", "f")
	whole, _ := jsonSize(full)
	r, err := FitContextPage(full, whole-1, jsonSize)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated || len(r.Documents) != 5 || r.NextCursor != "e" {
		t.Fatalf("trimmed page: %d documents, cursor %q, truncated %v", len(r.Documents), r.NextCursor, r.Truncated)
	}
	if n, _ := jsonSize(r); n > whole-1 {
		t.Fatalf("page of %d bytes exceeds budget", n)
	}
	if len(full.Documents) != 6 {
		t.Fatal("trimming changed the caller's page")
	}
}

func TestFitContextPageInterleavesKindsByID(t *testing.T) {
	r := ContextResult{
		Documents: []ContextDoc{{ID: "a"}, {ID: "c"}},
		Tasks:     []TaskEntry{{ID: "b"}, {ID: "d"}},
	}
	whole, _ := jsonSize(r)
	r, err := FitContextPage(r, whole-1, jsonSize)
	if err != nil || len(r.Tasks) != 1 || len(r.Documents) != 2 || r.NextCursor != "c" {
		t.Fatalf("highest ID was not dropped first: %+v, %v", r, err)
	}
}

func TestFitContextPageCompactsTheLastEntryBeforeFailing(t *testing.T) {
	long := strings.Repeat("é", 400)
	r := ContextResult{Documents: []ContextDoc{{ID: "a", Title: long, Purpose: long, Source: long}}}
	compacted, err := FitContextPage(r, 2048, jsonSize)
	if err != nil || compacted.Documents[0].Title != CompactText(long) || compacted.Truncated {
		t.Fatalf("single entry was not compacted: %v", err)
	}
	if r.Documents[0].Title != long {
		t.Fatal("compaction changed the caller's entry")
	}
	if _, err := FitContextPage(r, 16, jsonSize); Classify(err).Code != "size-limit" {
		t.Fatalf("an entry that cannot fit: %v", err)
	}
}
