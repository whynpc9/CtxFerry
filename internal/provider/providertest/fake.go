// Package providertest supplies synthetic provider and secure-store fixtures.
package providertest

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

const SyntheticSession = "ctxferry-synthetic-provider-session"

type Reference struct {
	Profile provider.Profile
	Locator provider.Locator
}

// Configure Entries and Methods before sharing the fake. The fake keys both
// credentials and entries by their complete reference.
type Fake struct {
	Entries map[Reference]string
	Methods []provider.MethodCapabilities

	mu         sync.Mutex
	states     map[provider.Profile]provider.AuthState
	loginCount int
	lookups    []Reference
}

func (f *Fake) Capabilities() []provider.MethodCapabilities { return slices.Clone(f.Methods) }

func (f *Fake) SetState(p provider.Profile, state provider.AuthState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setState(p, state)
}

func (f *Fake) setState(p provider.Profile, state provider.AuthState) {
	if f.states == nil {
		f.states = make(map[provider.Profile]provider.AuthState)
	}
	f.states[p] = state
}

func (f *Fake) LoginCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loginCount
}

func (f *Fake) Lookups() []Reference {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.lookups)
}

func fakeSession(p provider.Profile) []byte {
	return []byte(SyntheticSession + ":" + p.Provider + ":" + p.Name + ":" + p.Method)
}

func (f *Fake) Login(_ context.Context, p provider.Profile) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loginCount++
	if !slices.ContainsFunc(f.Methods, func(m provider.MethodCapabilities) bool { return m.Method == p.Method }) {
		return nil, provider.ErrAuthenticationRequired
	}
	f.setState(p, provider.Authenticated)
	return fakeSession(p), nil
}

func (f *Fake) Authentication(_ context.Context, p provider.Profile, session []byte) (provider.Authentication, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return provider.Authentication{State: f.authState(p, session)}, nil
}

func (f *Fake) authState(p provider.Profile, session []byte) provider.AuthState {
	if !bytes.Equal(session, fakeSession(p)) || f.states[p] == "" {
		return provider.SignedOut
	}
	return f.states[p]
}

func (f *Fake) Resolve(_ context.Context, p provider.Profile, session []byte, l provider.Locator) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref := Reference{p, l}
	f.lookups = append(f.lookups, ref)
	if f.authState(p, session) != provider.Authenticated {
		return nil, provider.ErrAuthenticationRequired
	}
	value, ok := f.Entries[ref]
	if !ok {
		return nil, provider.ErrSecretUnavailable
	}
	return []byte(value), nil
}

// MemoryStore represents the secure-store seam in tests. It never writes files.
type MemoryStore struct {
	Unavailable bool
	mu          sync.Mutex
	sessions    map[provider.SessionKey][]byte
}

func (s *MemoryStore) Available(context.Context) error {
	if s.Unavailable {
		return errors.New("secure store unavailable")
	}
	return nil
}

func (s *MemoryStore) Load(ctx context.Context, key provider.SessionKey) ([]byte, error) {
	if err := s.Available(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[key]
	if !ok {
		return nil, provider.ErrNoSession
	}
	return bytes.Clone(session), nil
}

func (s *MemoryStore) Save(ctx context.Context, key provider.SessionKey, session []byte) error {
	if err := s.Available(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[provider.SessionKey][]byte)
	}
	s.sessions[key] = bytes.Clone(session)
	return nil
}
