// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package browserbridge

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadNativeMessageUsesLittleEndianLengthPrefix(t *testing.T) {
	payload := []byte(`{"id":"one","ok":true}`)
	var framed bytes.Buffer
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload)))
	framed.Write(header[:])
	framed.Write(payload)

	got, err := readNativeMessage(&framed, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}
