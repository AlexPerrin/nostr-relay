package main

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// connectOrSkip dials the relay and skips the test if it is not reachable.
func connectOrSkip(t *testing.T) *Client {
	t.Helper()
	c, err := Connect("ws://localhost:9090/")
	if err != nil {
		t.Skipf("relay not available at ws://localhost:9090/ — start it first: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// mustGenerateKey generates a key or fails the test.
func mustGenerateKey(t *testing.T) keyGeneratedMsg {
	t.Helper()
	cmd := generateKeyCmd("ws://localhost:9090")
	msg, ok := cmd().(keyGeneratedMsg)
	if !ok {
		t.Fatalf("generateKeyCmd returned unexpected message type")
	}
	return msg
}

// ── key generation ────────────────────────────────────────────────────────────

func TestGenerateKeyCmdReturnsKeyGeneratedMsg(t *testing.T) {
	msg := mustGenerateKey(t)

	if msg.privKey == nil {
		t.Fatal("privKey is nil")
	}
	if len(msg.privHex) != 64 {
		t.Fatalf("privHex length = %d, want 64", len(msg.privHex))
	}
	if len(msg.pubKey) != 64 {
		t.Fatalf("pubKey length = %d, want 64", len(msg.pubKey))
	}
	if msg.relayURL != "ws://localhost:9090" {
		t.Fatalf("relayURL = %q, want %q", msg.relayURL, "ws://localhost:9090")
	}
}

func TestGenerateKeyCmdProducesUniqueKeys(t *testing.T) {
	a := mustGenerateKey(t)
	b := mustGenerateKey(t)
	if a.privHex == b.privHex {
		t.Error("two generated private keys are identical — RNG broken?")
	}
	if a.pubKey == b.pubKey {
		t.Error("two generated public keys are identical")
	}
}

func TestGenerateKeyCmdPrivHexRoundTrips(t *testing.T) {
	msg := mustGenerateKey(t)

	// The hex in the message must parse back to the same key.
	parsed, err := parsePrivKey(msg.privHex)
	if err != nil {
		t.Fatalf("parsePrivKey: %v", err)
	}

	want := hex.EncodeToString(msg.privKey.Serialize())
	got := hex.EncodeToString(parsed.Serialize())
	if want != got {
		t.Errorf("round-trip mismatch: want %s, got %s", want, got)
	}
}

// ── parsePrivKey ─────────────────────────────────────────────────────────────

func TestParsePrivKeyValid(t *testing.T) {
	// Use a known 32-byte value.
	raw := strings.Repeat("ab", 32) // 64 hex chars
	k, err := parsePrivKey(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k == nil {
		t.Fatal("returned nil key")
	}
}

func TestParsePrivKeyTrimsWhitespace(t *testing.T) {
	raw := "  " + strings.Repeat("cd", 32) + "\n"
	if _, err := parsePrivKey(raw); err != nil {
		t.Fatalf("unexpected error with surrounding whitespace: %v", err)
	}
}

func TestParsePrivKeyInvalidHex(t *testing.T) {
	if _, err := parsePrivKey("not-hex"); err == nil {
		t.Error("expected error for invalid hex, got nil")
	}
}

func TestParsePrivKeyWrongLength(t *testing.T) {
	short := strings.Repeat("ab", 16) // 32 hex chars, only 16 bytes
	if _, err := parsePrivKey(short); err == nil {
		t.Error("expected error for 16-byte key, got nil")
	}
}

func TestParsePrivKeyEmpty(t *testing.T) {
	if _, err := parsePrivKey(""); err == nil {
		t.Error("expected error for empty string, got nil")
	}
}

// ── SignEvent ─────────────────────────────────────────────────────────────────

func TestSignEventSetsFields(t *testing.T) {
	msg := mustGenerateKey(t)

	e := &Event{
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Tags:      [][]string{},
		Content:   "hello",
	}
	if err := SignEvent(e, msg.privKey); err != nil {
		t.Fatalf("SignEvent: %v", err)
	}

	if e.PubKey == "" {
		t.Error("PubKey not set")
	}
	if e.ID == "" {
		t.Error("ID not set")
	}
	if e.Sig == "" {
		t.Error("Sig not set")
	}
	if e.PubKey != msg.pubKey {
		t.Errorf("PubKey = %s, want %s", e.PubKey, msg.pubKey)
	}
}

func TestSignEventIDIsCorrect(t *testing.T) {
	msg := mustGenerateKey(t)
	e := &Event{
		CreatedAt: 1700000000,
		Kind:      1,
		Tags:      [][]string{},
		Content:   "deterministic content",
	}
	if err := SignEvent(e, msg.privKey); err != nil {
		t.Fatalf("SignEvent: %v", err)
	}

	// Re-compute independently and compare.
	computed, err := e.computeID()
	if err != nil {
		t.Fatalf("computeID: %v", err)
	}
	if e.ID != computed {
		t.Errorf("stored ID %s != computed ID %s", e.ID, computed)
	}
}

func TestSignEventIDIs64HexChars(t *testing.T) {
	msg := mustGenerateKey(t)
	e := &Event{CreatedAt: time.Now().Unix(), Kind: 1, Tags: [][]string{}, Content: "x"}
	if err := SignEvent(e, msg.privKey); err != nil {
		t.Fatal(err)
	}
	if len(e.ID) != 64 {
		t.Errorf("ID length = %d, want 64", len(e.ID))
	}
	if _, err := hex.DecodeString(e.ID); err != nil {
		t.Errorf("ID is not valid hex: %v", err)
	}
}

func TestSignEventSigIs128HexChars(t *testing.T) {
	msg := mustGenerateKey(t)
	e := &Event{CreatedAt: time.Now().Unix(), Kind: 1, Tags: [][]string{}, Content: "x"}
	if err := SignEvent(e, msg.privKey); err != nil {
		t.Fatal(err)
	}
	if len(e.Sig) != 128 {
		t.Errorf("Sig length = %d, want 128", len(e.Sig))
	}
}

// ── integration: connect ──────────────────────────────────────────────────────

func TestConnect(t *testing.T) {
	connectOrSkip(t) // skips cleanly if relay is down
}

// ── integration: publish ─────────────────────────────────────────────────────

func TestPublishAccepted(t *testing.T) {
	client := connectOrSkip(t)
	km := mustGenerateKey(t)

	e := &Event{
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Tags:      [][]string{},
		Content:   "TestPublishAccepted",
	}
	if err := SignEvent(e, km.privKey); err != nil {
		t.Fatal(err)
	}

	accepted, msg, err := client.Publish(e)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !accepted {
		t.Errorf("event rejected: %s", msg)
	}
}

func TestPublishDuplicateIsAccepted(t *testing.T) {
	client := connectOrSkip(t)
	km := mustGenerateKey(t)

	e := &Event{
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Tags:      [][]string{},
		Content:   "TestPublishDuplicate",
	}
	if err := SignEvent(e, km.privKey); err != nil {
		t.Fatal(err)
	}

	if _, _, err := client.Publish(e); err != nil {
		t.Fatal(err)
	}
	accepted, msg, err := client.Publish(e)
	if err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	if !accepted {
		t.Errorf("duplicate should be accepted, got rejected: %s", msg)
	}
}

func TestPublishInvalidSignatureRejected(t *testing.T) {
	client := connectOrSkip(t)
	km := mustGenerateKey(t)

	e := &Event{
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Tags:      [][]string{},
		Content:   "tampered",
	}
	if err := SignEvent(e, km.privKey); err != nil {
		t.Fatal(err)
	}
	// Corrupt the signature.
	e.Sig = strings.Repeat("00", 64)

	accepted, _, err := client.Publish(e)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if accepted {
		t.Error("corrupted signature should be rejected")
	}
}

// ── integration: subscribe + receive ─────────────────────────────────────────

func TestSubscribeReceivesPublishedEvent(t *testing.T) {
	client := connectOrSkip(t)
	km := mustGenerateKey(t)

	ch := client.Subscribe("test-sub", Filter{
		Authors: []string{km.pubKey},
		Kinds:   []int{1},
	})
	defer client.CloseSubscription("test-sub")

	want := "TestSubscribeReceivesPublishedEvent-" + km.privHex[:8]
	e := &Event{
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Tags:      [][]string{},
		Content:   want,
	}
	if err := SignEvent(e, km.privKey); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Publish(e); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-ch:
		if got.Content != want {
			t.Errorf("content = %q, want %q", got.Content, want)
		}
		if got.PubKey != km.pubKey {
			t.Errorf("pubkey = %s, want %s", got.PubKey, km.pubKey)
		}
		if got.ID != e.ID {
			t.Errorf("id = %s, want %s", got.ID, e.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSubscribeFilterByKind(t *testing.T) {
	client := connectOrSkip(t)
	km := mustGenerateKey(t)

	// Subscribe only to kind 2 — we will publish kind 1, expecting nothing.
	ch := client.Subscribe("kind-filter-sub", Filter{
		Authors: []string{km.pubKey},
		Kinds:   []int{2},
	})
	defer client.CloseSubscription("kind-filter-sub")

	e := &Event{
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Tags:      [][]string{},
		Content:   "should not arrive",
	}
	if err := SignEvent(e, km.privKey); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Publish(e); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-ch:
		t.Errorf("received unexpected event: %+v", ev)
	case <-time.After(300 * time.Millisecond):
		// Correct: nothing arrived.
	}
}

func TestCloseSubscriptionStopsDelivery(t *testing.T) {
	client := connectOrSkip(t)
	km := mustGenerateKey(t)

	ch := client.Subscribe("close-sub", Filter{
		Authors: []string{km.pubKey},
		Kinds:   []int{1},
	})

	client.CloseSubscription("close-sub")

	// Channel must be closed.
	select {
	case _, open := <-ch:
		if open {
			t.Error("channel should be closed after CloseSubscription")
		}
	case <-time.After(time.Second):
		t.Error("channel was not closed after CloseSubscription")
	}
}
