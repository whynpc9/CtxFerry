package infisical

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

func TestCreateImport(t *testing.T) {
	const value = "placeholder-only"
	p := provider.Profile{Provider: Name, Name: "fixture", Method: universalAuth}
	session := signedFixture(universalAuth, time.Now().Add(time.Hour))
	for _, tc := range []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{name: "created", status: 200},
		{name: "approval", status: 200, body: `{"approval":{"id":"pending"}}`, err: provider.ErrImportPending},
		{name: "unrecognized success", status: 200, body: `{"secret":{"secretValue":"placeholder-only"}}`, err: provider.ErrImportUnknown},
		{name: "collision", status: 400, body: `{"message":"Secret 'TEST_KEY' already exists in path '/ctxferry' of environment 'sandbox'"}`, err: provider.ErrImportCollision},
		{name: "validation", status: 400, body: `{"message":"placeholder-only"}`, err: provider.ErrImportRejected},
		{name: "session", status: 401, err: provider.ErrAuthenticationRequired},
		{name: "denied", status: 403, err: provider.ErrImportRejected},
		{name: "server failure", status: 500, err: provider.ErrImportUnknown},
		{name: "redirect", status: 307, err: provider.ErrImportUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]string
				if r.Method != http.MethodPost || r.URL.Path != secrets+"/"+namedEntry.Key || r.URL.RawQuery != "" ||
					r.Header.Get("Authorization") != "Bearer "+string(session) || r.Header.Get("Content-Type") != "application/json" ||
					json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 5 ||
					body["projectId"] != namedEntry.ProjectID || body["environment"] != namedEntry.Environment ||
					body["secretPath"] != namedEntry.Path || body["type"] != "shared" || body["secretValue"] != value {
					t.Error("request did not use the exact create-only route, scope and body")
				}
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(tc.status)
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				} else if tc.status == 200 {
					writeSecret(w, namedEntry, fixtureID, value, false)
				}
			}))
			defer server.Close()
			a := newAdapter(server.URL)
			if err := a.Import(t.Context(), p, session, namedEntry, []byte(value)); !errors.Is(err, tc.err) {
				t.Errorf("create import did not return the expected status %v", tc.err)
			}
			for _, unsupported := range []string{" leading", "trailing ", "${reference}", "multiline\nvalue"} {
				if err := a.Import(t.Context(), p, session, namedEntry, []byte(unsupported)); !errors.Is(err, provider.ErrImportRejected) {
					t.Error("expected rejection of value that the API would transform")
				}
			}
			if err := a.Import(t.Context(), p, session, provider.Locator{EntryID: fixtureID}, []byte(value)); !errors.Is(err, provider.ErrImportRejected) {
				t.Error("expected rejection of entry ID")
			}
			if calls.Load() != 1 {
				t.Error("expected exactly one HTTP request")
			}
		})
	}
}
