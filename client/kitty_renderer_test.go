package main

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestWriteKittyChunkedSingleChunk(t *testing.T) {
	// Redirect kittyWriter to a buffer for testing
	var buf bytes.Buffer
	kittyWriter.Reset(&buf)

	data := []byte("AAAA")
	err := writeKittyChunked(data, "f=100,a=T,i=1")
	if err != nil {
		t.Fatalf("writeKittyChunked failed: %v", err)
	}
	kittyWriter.Flush()

	output := buf.String()
	// Should contain APC, control data, semicolon, payload, ST
	if output != "\033_Gf=100,a=T,i=1;AAAA\033\\" {
		t.Errorf("unexpected output: %q", output)
	}
}

func TestWriteKittyChunkedMultiChunk(t *testing.T) {
	var buf bytes.Buffer
	kittyWriter.Reset(&buf)

	// Data larger than kittyChunkSize to force chunking
	data := make([]byte, kittyChunkSize*2+100)
	for i := range data {
		data[i] = 'A'
	}

	err := writeKittyChunked(data, "f=100")
	if err != nil {
		t.Fatalf("writeKittyChunked failed: %v", err)
	}
	kittyWriter.Flush()

	output := buf.String()

	// First chunk should have control data + m=1
	if !bytes.Contains([]byte(output), []byte("\033_Gf=100,m=1;")) {
		t.Error("first chunk missing control data with m=1")
	}

	// Should have middle chunk(s) with m=1
	if !bytes.Contains([]byte(output), []byte("\033_Gm=1,q=2;")) {
		t.Error("missing middle chunk with m=1")
	}

	// Final chunk should have m=0
	if !bytes.Contains([]byte(output), []byte("\033_Gm=0,q=2;")) {
		t.Error("missing final chunk with m=0")
	}
}

func TestWriteKittyChunkedExactBoundary(t *testing.T) {
	var buf bytes.Buffer
	kittyWriter.Reset(&buf)

	data := make([]byte, kittyChunkSize)
	for i := range data {
		data[i] = 'B'
	}

	err := writeKittyChunked(data, "f=100")
	if err != nil {
		t.Fatalf("writeKittyChunked failed: %v", err)
	}
	kittyWriter.Flush()

	output := buf.String()
	// Exactly kittyChunkSize should be single chunk (no m= flag)
	if bytes.Contains([]byte(output), []byte("m=")) {
		t.Error("exact boundary should produce single chunk without m= flag")
	}
}

func TestWriteKittyChunkedEmpty(t *testing.T) {
	var buf bytes.Buffer
	kittyWriter.Reset(&buf)

	err := writeKittyChunked([]byte{}, "f=100")
	if err != nil {
		t.Fatalf("writeKittyChunked failed on empty data: %v", err)
	}
	kittyWriter.Flush()

	output := buf.String()
	if output != "\033_Gf=100;\033\\" {
		t.Errorf("unexpected output for empty data: %q", output)
	}
}

func TestKittyB64BufReuse(t *testing.T) {
	kittyB64Buf = nil

	// First encode — allocates buffer
	small := make([]byte, 100)
	b64Len := base64.StdEncoding.EncodedLen(len(small))
	kittyB64Buf = make([]byte, b64Len)
	base64.StdEncoding.Encode(kittyB64Buf, small)
	firstCap := cap(kittyB64Buf)

	// Second encode with same size — should reuse
	b64Len2 := base64.StdEncoding.EncodedLen(len(small))
	if cap(kittyB64Buf) < b64Len2 {
		t.Fatal("buffer should have been reused")
	}
	kittyB64Buf = kittyB64Buf[:b64Len2]
	base64.StdEncoding.Encode(kittyB64Buf, small)

	if cap(kittyB64Buf) != firstCap {
		t.Errorf("buffer was reallocated: cap went from %d to %d", firstCap, cap(kittyB64Buf))
	}
}

func TestGetKittyStatsEmpty(t *testing.T) {
	kStats.mu.Lock()
	kStats.framesRendered = 0
	kStats.totalEncodeNs = 0
	kStats.totalWriteNs = 0
	kStats.totalBytes = 0
	kStats.totalBase64 = 0
	kStats.mu.Unlock()

	frames, avgEncode, avgWrite, avgBytes := GetKittyStats()
	if frames != 0 {
		t.Errorf("expected 0 frames, got %d", frames)
	}
	if avgEncode != 0 || avgWrite != 0 || avgBytes != 0 {
		t.Error("expected zero averages for empty stats")
	}
}

func TestKittyFramePreservesCursorAndToolbarOrigin(t *testing.T) {
	uiScreen(t, 80, 24)
	oldCfg := cfg
	cfg = &Config{}
	t.Cleanup(func() { cfg = oldCfg })
	var buf bytes.Buffer
	kittyWriter.Reset(&buf)
	if err := displayWithKittyPNG([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("\033[s\033[3;2H")) || !bytes.HasSuffix(buf.Bytes(), []byte("\033[u")) {
		t.Fatalf("graphics disturb toolbar or cursor: %q", buf.String())
	}
}
