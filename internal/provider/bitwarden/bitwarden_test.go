package bitwarden

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/providertest"
)

const itemID = "32f7cf3b-cb32-4944-8eeb-17d088b5a588"
const password = " synthetic password with whitespace\n"
const sessionKey = "synthetic-session"

func TestProviderContract(t *testing.T) {
	t.Setenv(profileEnv, "team")
	t.Setenv(sessionEnv, sessionKey)
	p := provider.Profile{Provider: Name, Name: "team", Method: Method}
	state := provider.Authenticated
	item := provider.Locator{EntryID: itemID}
	near := provider.Locator{EntryID: "32f7cf3b-cb32-4944-8eeb-17d088b5a589"}
	failed := errors.New("synthetic command failure")
	a := &adapter{run: func(_ context.Context, session []byte, args ...string) ([]byte, error) {
		switch {
		case slices.Equal(args, []string{"status", "--nointeraction"}):
			if len(session) == 0 || state == provider.Expired {
				return []byte(`{"status":"unauthenticated"}`), nil
			}
			if string(session) == sessionKey && state == provider.Authenticated {
				return []byte(`{"status":"unlocked"}`), nil
			}
			return []byte(`{"status":"locked"}`), nil
		case slices.Equal(args, []string{"get", "password", itemID, "--response", "--nointeraction"}):
			if state == provider.Authenticated {
				return []byte(`{"success":true,"data":{"object":"string","data":" synthetic password with whitespace\n"}}`), nil
			}
			if state == provider.Expired {
				return []byte(`{"success":false,"message":"You are not logged in."}`), failed
			}
			return []byte(`{"success":false,"message":"Vault is locked."}`), failed
		case slices.Equal(args, []string{"get", "password", near.EntryID, "--response", "--nointeraction"}):
			return []byte(`{"success":false,"message":"Not found."}`), failed
		default:
			t.Fatal("unexpected bw invocation")
			return nil, failed
		}
	}}
	providertest.RunContract(t, providertest.ContractFixture{
		Adapter: a, Profile: p, Entry: item, NearMatch: near, Value: password,
		Capabilities:     []provider.MethodCapabilities{{Method: Method, OfflineRead: true}},
		SetState:         func(next provider.AuthState) { state = next },
		InvalidatedState: provider.SignedOut,
	})
}

func TestAuthenticationEvidence(t *testing.T) {
	t.Setenv(profileEnv, "")
	p := provider.Profile{Provider: Name, Name: "default", Method: Method}
	for _, tc := range []struct {
		status             string
		session            string
		state              provider.AuthState
		loggedIn, unlocked bool
		validity           provider.SessionValidity
	}{
		{"unauthenticated", "", provider.SignedOut, false, false, provider.SessionAbsent},
		{"unauthenticated", sessionKey, provider.SignedOut, false, false, provider.SessionInvalid},
		{"locked", "", provider.Locked, true, false, provider.SessionAbsent},
		{"locked", sessionKey, provider.Locked, true, false, provider.SessionInvalid},
		{"unlocked", sessionKey, provider.Authenticated, true, true, provider.SessionValid},
		{"unlocked", "", provider.Locked, true, true, provider.SessionAbsent},
	} {
		a := &adapter{run: func(_ context.Context, session []byte, _ ...string) ([]byte, error) {
			if string(session) != tc.session {
				t.Fatal("CLI received a different session")
			}
			return []byte(`{"status":"` + tc.status + `"}`), nil
		}}
		auth, err := a.Authentication(t.Context(), p, []byte(tc.session))
		if err != nil || auth.State != tc.state || auth.ExpiresAt != nil || auth.SessionValidity != tc.validity ||
			auth.LoggedIn == nil || *auth.LoggedIn != tc.loggedIn || auth.Unlocked == nil || *auth.Unlocked != tc.unlocked {
			t.Fatalf("unexpected authentication evidence for %s and %s: %+v", tc.status, tc.validity, auth)
		}
	}
}

func TestManagementURLUsesLocalWebVault(t *testing.T) {
	t.Setenv(profileEnv, "team")
	profile := provider.Profile{Provider: Name, Name: "team", Method: Method}
	ref := provider.Locator{EntryID: itemID}
	for _, tc := range []struct {
		name, status, origin string
	}{
		{"cloud", `{"serverUrl":null}`, "https://vault.bitwarden.com/"},
		{"self-hosted", `{"serverUrl":"https://vault.example.invalid/team"}`, "https://vault.example.invalid/team/"},
		{"untrusted credentials", `{"serverUrl":"https://fake-credential@vault.example.invalid"}`, ""},
		{"untrusted scheme", `{"serverUrl":"file:///vault"}`, ""},
		{"untrusted query", `{"serverUrl":"https://vault.example.invalid/?token=private-value"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &adapter{run: func(_ context.Context, session []byte, args ...string) ([]byte, error) {
				if len(session) != 0 || !slices.Equal(args, []string{"status", "--nointeraction"}) {
					t.Fatal("opening management page granted unexpected provider access")
				}
				return []byte(tc.status), nil
			}}
			link, err := a.ManagementURL(t.Context(), profile, ref)
			if tc.origin == "" {
				if err == nil || link != "" {
					t.Fatal("unsafe management link was accepted")
				}
				return
			}
			if err != nil || link != tc.origin+"#/vault?itemId="+itemID {
				t.Fatalf("unexpected web vault destination: %v", err)
			}
		})
	}
}

func TestResolveUnavailable(t *testing.T) {
	t.Setenv(profileEnv, "")
	p := provider.Profile{Provider: Name, Name: "default", Method: Method}
	for _, tc := range []struct {
		name, response string
		want           error
	}{
		{"signed out", `{"success":false,"message":"You are not logged in."}`, provider.ErrAuthenticationRequired},
		{"locked", `{"success":false,"message":"Vault is locked."}`, provider.ErrAuthenticationRequired},
		{"missing", `{"success":false,"message":"Not found."}`, provider.ErrSecretUnavailable},
		{"denied", `{"success":false,"message":"Access to this item type is restricted by organizational policy."}`, provider.ErrSecretUnavailable},
		{"hidden password", `{"success":true,"data":{"object":"string","data":null}}`, provider.ErrSecretUnavailable},
		{"unstructured", "diagnostic with synthetic private data", provider.ErrSecretUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &adapter{run: func(context.Context, []byte, ...string) ([]byte, error) {
				return []byte(tc.response), nil
			}}
			value, err := a.Resolve(t.Context(), p, []byte(sessionKey), provider.Locator{EntryID: itemID})
			if len(value) != 0 || !errors.Is(err, tc.want) || err.Error() != tc.want.Error() {
				t.Fatal("resolve did not return a sanitized error with no value")
			}
		})
	}
	a := &adapter{run: func(context.Context, []byte, ...string) ([]byte, error) {
		t.Fatal("inexact locator reached bw")
		return nil, nil
	}}
	for _, ref := range []provider.Locator{
		{EntryID: "similar item"},
		{EntryID: "--session"},
		{EntryID: itemID, Key: "wrong"},
		{EntryID: "{" + itemID + "}"},
	} {
		if _, err := a.Resolve(t.Context(), p, []byte(sessionKey), ref); !errors.Is(err, provider.ErrSecretUnavailable) {
			t.Fatal("accepted an inexact locator")
		}
	}
}
