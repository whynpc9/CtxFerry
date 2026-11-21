package app

import "unicode/utf8"

// ContextShowBudget is the agent-facing byte budget of one context show page.
const ContextShowBudget = 8192

// FitContextPage trims a context show page until size reports that it fits
// within budget. size measures the caller's complete representation of the
// page, never a fragment, because escaping can expand metadata severalfold.
//
// Entries are dropped from the highest ID down, and NextCursor points at the
// last entry kept, so the next page resumes after it. Every nonempty page
// keeps one retrievable entry: a single remaining entry is compacted, and if
// it still does not fit the page fails with size-limit.
func FitContextPage(r ContextResult, budget int, size func(ContextResult) (int, error)) (ContextResult, error) {
	compact := false
	for {
		n, err := size(r)
		if err != nil {
			return r, err
		}
		if n <= budget {
			return r, nil
		}
		if len(r.Documents)+len(r.Resources)+len(r.Tasks) <= 1 {
			if compact {
				return r, Fail("size-limit", "required context metadata exceeds 8192-byte show budget")
			}
			compact = true
			compactContextPage(&r)
			continue
		}
		doc, resource, task := lastContextIDs(r)
		switch max(doc, resource, task) {
		case doc:
			r.Documents = r.Documents[:len(r.Documents)-1]
		case resource:
			r.Resources = r.Resources[:len(r.Resources)-1]
		case task:
			r.Tasks = r.Tasks[:len(r.Tasks)-1]
		}
		r.Truncated = true
		doc, resource, task = lastContextIDs(r)
		r.NextCursor = max(doc, resource, task)
	}
}

// compactContextPage copies the entries it shortens, leaving the caller's intact.
func compactContextPage(r *ContextResult) {
	compactPtr := func(s *string) *string {
		if s == nil {
			return nil
		}
		c := CompactText(*s)
		return &c
	}
	r.Summary = compactPtr(r.Summary)
	if len(r.Documents) == 1 {
		d := r.Documents[0]
		d.Title, d.Purpose, d.Source = CompactText(d.Title), CompactText(d.Purpose), CompactText(d.Source)
		d.Summary = compactPtr(d.Summary)
		r.Documents = []ContextDoc{d}
	}
	if len(r.Resources) == 1 {
		p := r.Resources[0]
		p.Identity, p.Purpose = CompactText(p.Identity), CompactText(p.Purpose)
		r.Resources = []ResourcePurpose{p}
	}
	if len(r.Tasks) == 1 {
		t := r.Tasks[0]
		t.Purpose = CompactText(t.Purpose)
		r.Tasks = []TaskEntry{t}
	}
}

// CompactText shortens s to at most 64 bytes of valid UTF-8 plus an ellipsis.
func CompactText(s string) string {
	if len(s) <= 64 {
		return s
	}
	s = s[:64]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

func lastContextIDs(r ContextResult) (doc, resource, task string) {
	if len(r.Documents) > 0 {
		doc = r.Documents[len(r.Documents)-1].ID
	}
	if len(r.Resources) > 0 {
		resource = r.Resources[len(r.Resources)-1].ID
	}
	if len(r.Tasks) > 0 {
		task = r.Tasks[len(r.Tasks)-1].ID
	}
	return
}
