package infisical

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/providertest"
)

var namedEntry = provider.Locator{ProjectID: "project-123", Environment: "sandbox", Path: "/ctxferry", Key: "TEST_KEY"}

const fixtureValue = "synthetic-value\n"
const fixtureID = "a19f8556-26df-4bd5-9782-526daed778dc"

func signedFixture(method string, expiry time.Time) []byte {
	kind := "accessToken"
	if method == universalAuth {
		kind = "identityAccessToken"
	}
	claims := fmt.Appendf(nil, `{"exp":%d,"authTokenType":%q}`, expiry.Unix(), kind)
	return []byte("fixture." + base64.RawURLEncoding.EncodeToString(claims) + ".signature")
}

func writeSecret(w http.ResponseWriter, l provider.Locator, id, value string, hidden bool) {
	_ = json.NewEncoder(w).Encode(map[string]any{"secret": map[string]any{
		"id": id, "workspace": l.ProjectID, "environment": l.Environment, "secretPath": l.Path,
		"secretKey": l.Key, "type": "shared", "secretValue": value, "secretValueHidden": hidden,
	}})
}

func TestProviderContract(t *testing.T) {
	methods := []provider.MethodCapabilities{
		{Method: personal, SessionExpiry: true},
		{Method: universalAuth, MachineAuth: true, SessionExpiry: true, CreateImport: true},
	}
	for _, method := range methods {
		t.Run(method.Method, func(t *testing.T) {
			profile := provider.Profile{Provider: Name, Name: "fixture", Method: method.Method}
			now := time.Now()
			token := signedFixture(method.Method, now.Add(time.Hour))
			var state atomic.Value
			state.Store(provider.Authenticated)
			statusEndpoint := personalStatus
			entry := namedEntry
			if method.MachineAuth {
				statusEndpoint = machineStatus
				entry = provider.Locator{EntryID: fixtureID}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == machineLogin && method.MachineAuth {
					var login map[string]string
					if r.Method != http.MethodPost || r.Header.Get("Authorization") != "" ||
						json.NewDecoder(r.Body).Decode(&login) != nil || login["clientId"] != "fixture-client" ||
						login["clientSecret"] != "fixture-client-secret" {
						http.Error(w, "bad login request", http.StatusBadRequest)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"accessToken": string(token)})
					return
				}
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+string(token) {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch state.Load() {
				case provider.Locked:
					if r.URL.Path == statusEndpoint {
						w.WriteHeader(http.StatusForbidden)
					} else {
						w.WriteHeader(http.StatusUnauthorized)
					}
					return
				case provider.Expired:
					t.Error("an expired JWT should be rejected before an API request")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch r.URL.Path {
				case personalStatus:
					_, _ = fmt.Fprint(w, `{"user":{"id":"fixture-user"}}`)
				case machineStatus:
					_, _ = fmt.Fprint(w, `{"identityDetails":{"organization":{"id":"fixture-org"}}}`)
				case secrets + "/" + namedEntry.Key:
					q := r.URL.Query()
					if q.Get("projectId") != namedEntry.ProjectID || q.Get("environment") != namedEntry.Environment ||
						q.Get("secretPath") != namedEntry.Path || q.Get("includeImports") != "false" ||
						q.Get("expandSecretReferences") != "false" || q.Get("type") != "shared" || q.Get("viewSecretValue") != "true" {
						t.Error("named request omitted an exact scope or safety parameter")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					writeSecret(w, namedEntry, fixtureID, fixtureValue, false)
				case secrets + "/id/" + fixtureID:
					if r.URL.Query().Get("viewSecretValue") != "true" {
						t.Error("ID read did not request the secret value")
					}
					writeSecret(w, namedEntry, fixtureID, fixtureValue, false)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			a := newAdapter(server.URL)
			a.personalToken = func(_ context.Context, name string) ([]byte, error) {
				if name != profile.Name || method.MachineAuth {
					return nil, provider.ErrAuthenticationRequired
				}
				return append([]byte(nil), token...), nil
			}
			a.machineCredentials = func(name string) (string, string) {
				if name != profile.Name || !method.MachineAuth {
					return "", ""
				}
				return "fixture-client", "fixture-client-secret"
			}

			a.now = func() time.Time {
				if state.Load() == provider.Expired {
					return now.Add(2 * time.Hour)
				}
				return now
			}
			nearMatch := namedEntry
			nearMatch.Key += "_ABSENT"
			if method.MachineAuth {
				nearMatch = provider.Locator{EntryID: "a19f8556-26df-4bd5-9782-526daed778dd"}
			}
			providertest.RunContract(t, providertest.ContractFixture{
				Adapter: a, Profile: profile, Capabilities: methods, Entry: entry, NearMatch: nearMatch, Value: fixtureValue,
				SetState: func(next provider.AuthState) { state.Store(next) },
			})
		})
	}
}

func TestResolveRejectsUnavailableAndInexactResponses(t *testing.T) {
	profile := provider.Profile{Provider: Name, Name: "fixture", Method: personal}
	token := signedFixture(personal, time.Now().Add(time.Hour))
	other := namedEntry
	other.Environment = "development"
	for _, tc := range []struct {
		name     string
		status   int
		response func(http.ResponseWriter)
		locator  provider.Locator
		want     error
	}{
		{"permission denied", http.StatusForbidden, nil, namedEntry, provider.ErrSecretUnavailable},
		{"missing", http.StatusNotFound, nil, namedEntry, provider.ErrSecretUnavailable},
		{"revoked", http.StatusUnauthorized, nil, namedEntry, provider.ErrAuthenticationRequired},
		{"hidden", http.StatusOK, func(w http.ResponseWriter) { writeSecret(w, namedEntry, fixtureID, fixtureValue, true) }, namedEntry, provider.ErrSecretUnavailable},
		{"wrong environment", http.StatusOK, func(w http.ResponseWriter) { writeSecret(w, other, fixtureID, fixtureValue, false) }, namedEntry, provider.ErrSecretUnavailable},
		{"wrong id", http.StatusOK, func(w http.ResponseWriter) { writeSecret(w, namedEntry, fixtureID, fixtureValue, false) }, provider.Locator{EntryID: "different-id"}, provider.ErrSecretUnavailable},
		{"malformed", http.StatusOK, func(w http.ResponseWriter) { _, _ = fmt.Fprint(w, fixtureValue) }, namedEntry, provider.ErrSecretUnavailable},
		{"empty value", http.StatusOK, func(w http.ResponseWriter) { writeSecret(w, namedEntry, fixtureID, "", false) }, namedEntry, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if tc.response != nil {
					tc.response(w)
				} else {
					_, _ = fmt.Fprint(w, fixtureValue)
				}
			}))
			defer backend.Close()
			value, err := newAdapter(backend.URL).Resolve(t.Context(), profile, token, tc.locator)
			if len(value) != 0 || !errors.Is(err, tc.want) || err != nil && strings.Contains(err.Error(), strings.TrimSpace(fixtureValue)) {
				t.Fatal("unexpected value or diagnostic")
			}
		})
	}
}

func TestInvalidScopeAndWrongMethodStayLocal(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid request reached the backend")
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer backend.Close()
	a := newAdapter(backend.URL)
	profile := provider.Profile{Provider: Name, Name: "fixture", Method: universalAuth}
	if _, err := a.Resolve(t.Context(), profile, signedFixture(personal, time.Now().Add(time.Hour)), namedEntry); !errors.Is(err, provider.ErrAuthenticationRequired) {
		t.Fatal("a personal token was accepted for machine auth")
	}
	for _, ref := range []provider.Locator{
		{EntryID: fixtureID, Key: namedEntry.Key},
		{EntryID: "../TEST_KEY"},
		{ProjectID: "project", Path: "/ctxferry", Key: namedEntry.Key},
		{ProjectID: "project", Environment: "sandbox", Path: "/some/../ctxferry", Key: namedEntry.Key},
	} {
		if _, err := a.Resolve(t.Context(), profile, signedFixture(universalAuth, time.Now().Add(time.Hour)), ref); !errors.Is(err, provider.ErrSecretUnavailable) {
			t.Fatal("invalid locator was accepted")
		}
	}
}

func TestInjectedSessionRequiresExactScope(t *testing.T) {
	t.Setenv("CTXFERRY_INFISICAL_WORKSPACE_ID", "workspace")
	t.Setenv("CTXFERRY_INFISICAL_DEVICE_ID", "device")
	t.Setenv("CTXFERRY_INFISICAL_PROFILE", "headless")
	t.Setenv("CTXFERRY_INFISICAL_ACCESS_TOKEN", "injected-fixture")

	p := provider.Profile{Provider: Name, Name: "headless", Method: universalAuth}
	key := provider.SessionKey{WorkspaceID: "workspace", DeviceID: "device", Profile: p}
	if string(InjectedSession(key)) != "injected-fixture" {
		t.Fatal("the exact scoped token was unavailable")
	}
	for _, other := range []provider.SessionKey{
		{WorkspaceID: "peer", DeviceID: key.DeviceID, Profile: p},
		{WorkspaceID: key.WorkspaceID, DeviceID: "peer", Profile: p},
		{WorkspaceID: key.WorkspaceID, DeviceID: key.DeviceID, Profile: provider.Profile{Provider: Name, Name: "peer", Method: universalAuth}},
		{WorkspaceID: key.WorkspaceID, DeviceID: key.DeviceID, Profile: provider.Profile{Provider: Name, Name: p.Name, Method: personal}},
	} {
		if len(InjectedSession(other)) != 0 {
			t.Fatal("a token was returned outside its scope")
		}
	}
}
