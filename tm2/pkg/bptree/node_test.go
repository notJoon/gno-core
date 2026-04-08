package bptree

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"
)

func TestReadBytes_RejectsExcessiveLength(t *testing.T) {
	const claimedSize = 8 << 20 // 8MB

	var buf bytes.Buffer
	var uvarintBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(uvarintBuf[:], uint64(claimedSize))
	buf.Write(uvarintBuf[:n])

	r := bytes.NewReader(buf.Bytes())

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	_, err := readBytes(r)
	if err == nil {
		t.Fatal("expected error due to length exceeding reader, got nil")
	}

	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > 1<<20 {
		t.Fatalf("unexpected large allocation: %d bytes (should be near zero)", allocated)
	}
}

func TestReadBytes_ValidData(t *testing.T) {
	payload := []byte("hello, bptree")

	var buf bytes.Buffer
	var uvarintBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(uvarintBuf[:], uint64(len(payload)))
	buf.Write(uvarintBuf[:n])
	buf.Write(payload)

	r := bytes.NewReader(buf.Bytes())
	got, err := readBytes(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
}
