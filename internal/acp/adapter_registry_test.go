// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package acp

import "testing"

func TestAdapterRegistryStatusDiscoveryDoesNotActivateAgents(t *testing.T) {
	registry := NewAdapterRegistry(t.TempDir(), t.TempDir(), 2, 0)
	t.Cleanup(func() { _ = registry.Close() })

	statuses := registry.AdapterStatuses()
	if len(statuses) != 3 {
		t.Fatalf("AdapterStatuses() returned %d adapters, want 3", len(statuses))
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if len(registry.managers) != 0 {
		t.Fatalf("status discovery activated %d ACP managers", len(registry.managers))
	}
}
