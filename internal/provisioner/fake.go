// SPDX-License-Identifier: AGPL-3.0-or-later

package provisioner

import (
	"context"
	"sync"
)

// FakeProvisioner is a test double: instead of SSHing anywhere, it
// records the Targets passed to Provision and optionally invokes a
// hook so a test can simulate side effects (agent dialing in, etc.).
type FakeProvisioner struct {
	mu     sync.Mutex
	calls  []Target
	err    error
	onCall func(context.Context, Target) error
}

// NewFake builds a FakeProvisioner that returns nil from Provision.
func NewFake() *FakeProvisioner { return &FakeProvisioner{} }

// Provision records the call and returns the configured error (if any)
// or whatever the OnCall hook returns. Hook runs first; its error
// takes precedence so a test can simulate a remote failure.
func (f *FakeProvisioner) Provision(ctx context.Context, t Target) error {
	f.mu.Lock()
	f.calls = append(f.calls, t)
	hook := f.onCall
	staticErr := f.err
	f.mu.Unlock()

	if hook != nil {
		if err := hook(ctx, t); err != nil {
			return err
		}
	}
	return staticErr
}

// Calls returns a copy of the Targets seen so far.
func (f *FakeProvisioner) Calls() []Target {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Target, len(f.calls))
	copy(out, f.calls)
	return out
}

// SetError makes subsequent Provision calls return err.
func (f *FakeProvisioner) SetError(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

// SetOnCall installs a hook invoked on every Provision call. Use it
// to simulate the agent connecting back, advancing the clock, etc.
func (f *FakeProvisioner) SetOnCall(fn func(context.Context, Target) error) {
	f.mu.Lock()
	f.onCall = fn
	f.mu.Unlock()
}
