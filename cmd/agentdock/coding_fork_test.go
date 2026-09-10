// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCodingForkDoesNotUseOfficialUpdater(t *testing.T) {
	for _, args := range [][]string{{"update"}, {"update", "--check"}} {
		err := run(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "upstream-agentdock") {
			t.Fatalf("official update path remained active: %v", err)
		}
	}
}
