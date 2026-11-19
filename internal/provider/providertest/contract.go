package providertest

import (
	"errors"
	"slices"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

// ContractFixture points an adapter at provisioned synthetic entries. The
// fixture supplies a near match that must fail and backend-specific controls to
// lock and expire an established session.
type ContractFixture struct {
	Adapter      provider.Adapter
	Profile      provider.Profile
	Capabilities []provider.MethodCapabilities
	Entry        provider.Locator
	NearMatch    provider.Locator
	Value        string
	SetState     func(provider.AuthState)
}

func RunContract(t *testing.T, f ContractFixture) {
	t.Helper()
	ctx := t.Context()
	t.Run("capabilities by method", func(t *testing.T) {
		if got := f.Adapter.Capabilities(); !slices.Equal(got, f.Capabilities) {
			t.Fatalf("method capabilities = %+v, want %+v", got, f.Capabilities)
		}
	})
	assertAuth := func(t *testing.T, session []byte, state provider.AuthState) {
		t.Helper()
		auth, err := f.Adapter.Authentication(ctx, f.Profile, session)
		if err != nil || auth.State != state {
			t.Fatalf("authentication should report %s", state)
		}
	}
	assertUnavailable := func(t *testing.T, session []byte, ref provider.Locator, want error) {
		t.Helper()
		value, err := f.Adapter.Resolve(ctx, f.Profile, session, ref)
		if !errors.Is(err, want) || len(value) != 0 {
			t.Fatalf("resolve should return %s and no value", want)
		}
	}
	t.Run("authentication and exact reference", func(t *testing.T) {
		assertAuth(t, nil, provider.SignedOut)
		assertUnavailable(t, nil, f.Entry, provider.ErrAuthenticationRequired)
		session, err := f.Adapter.Login(ctx, f.Profile)
		defer clear(session)
		if err != nil || len(session) == 0 {
			t.Fatal("login did not return a session")
		}
		assertAuth(t, session, provider.Authenticated)
		value, err := f.Adapter.Resolve(ctx, f.Profile, session, f.Entry)
		defer clear(value)
		if err != nil || string(value) != f.Value {
			t.Fatal("resolve did not return the exact entry")
		}
		assertUnavailable(t, session, f.NearMatch, provider.ErrSecretUnavailable)
		for _, state := range []provider.AuthState{provider.Locked, provider.Expired} {
			f.SetState(state)
			assertAuth(t, session, state)
			assertUnavailable(t, session, f.Entry, provider.ErrAuthenticationRequired)
		}
	})
}
