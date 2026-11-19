package platform

import (
	"bytes"
	"errors"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/zalando/go-keyring"
)

func TestSystemCredentials(t *testing.T) {
	ctx := t.Context()
	key := provider.SessionKey{
		WorkspaceID: "workspace", DeviceID: "device-a",
		Profile: provider.Profile{Provider: "fake", Name: "team", Method: "machine"},
	}
	entries := map[[2]string]string{}
	storage := systemCredentials{
		get: func(service, account string) (string, error) {
			value, found := entries[[2]string{service, account}]
			if !found {
				return "", keyring.ErrNotFound
			}
			return value, nil
		},
		set: func(service, account, value string) error {
			entries[[2]string{service, account}] = value
			return nil
		},
	}
	if err := storage.Available(ctx); err != nil || len(entries) != 0 {
		t.Fatal("checking an available store must not write an entry")
	}
	if _, err := storage.Load(ctx, key); !errors.Is(err, provider.ErrNoSession) {
		t.Fatalf("unused profile should report no session: %v", err)
	}
	session := []byte("synthetic session")
	if err := storage.Save(ctx, key, session); err != nil {
		t.Fatal(err)
	}
	got, err := storage.Load(ctx, key)
	if err != nil || !bytes.Equal(got, session) {
		t.Fatal("session did not round-trip through the system store")
	}
	for _, other := range []provider.SessionKey{
		{WorkspaceID: "different-workspace", DeviceID: key.DeviceID, Profile: key.Profile},
		{WorkspaceID: key.WorkspaceID, DeviceID: "device-b", Profile: key.Profile},
		{WorkspaceID: key.WorkspaceID, DeviceID: key.DeviceID, Profile: provider.Profile{Provider: "fake", Name: "team", Method: "interactive"}},
	} {
		if _, err := storage.Load(ctx, other); !errors.Is(err, provider.ErrNoSession) {
			t.Fatal("session was available from a different workspace, device or method")
		}
	}
	storage.set = func(string, string, string) error { return nil }
	if err := storage.Save(ctx, key, []byte("different synthetic session")); !errors.Is(err, errCredentialStore) {
		t.Fatal("unconfirmed credential write was reported as successful")
	}
	storage.get = func(string, string) (string, error) { return "", errors.New("unavailable") }
	if err := storage.Available(ctx); !errors.Is(err, errCredentialStore) {
		t.Fatal("unavailable OS service accepted")
	}
}
