package providertest

import (
	"testing"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

func TestFakeProviderContract(t *testing.T) {
	profile := provider.Profile{Provider: "fake", Name: "demo", Method: "machine"}
	entry := provider.Locator{ProjectID: "vault", Environment: "stage", Path: "/app", Key: "API_KEY"}
	near := entry
	near.Path = "/other"
	capabilities := []provider.MethodCapabilities{
		{Method: "interactive", OfflineRead: true, FileDelivery: true},
		{Method: "machine", MachineAuth: true, NativeExec: true, SessionExpiry: true},
	}
	fake := &Fake{Methods: capabilities, Entries: map[Reference]string{{profile, entry}: "synthetic-value"}}
	RunContract(t, ContractFixture{
		Adapter: fake, Profile: profile, Capabilities: capabilities, Entry: entry, NearMatch: near, Value: "synthetic-value",
		SetState: func(state provider.AuthState) { fake.SetState(profile, state) },
	})
}
