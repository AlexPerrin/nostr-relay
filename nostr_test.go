package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
)

// Helper: generate a key pair and create a signed event.
func makeSignedEvent(t *testing.T, kind int, content string, tags [][]string) (*Event, *btcec.PrivateKey) {
	t.Helper()
	privKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return makeSignedEventWithKey(t, privKey, kind, content, tags), privKey
}

func makeSignedEventWithKey(t *testing.T, privKey *btcec.PrivateKey, kind int, content string, tags [][]string) *Event {
	t.Helper()
	if tags == nil {
		tags = [][]string{}
	}
	event := &Event{
		CreatedAt: time.Now().Unix(),
		Kind:      kind,
		Tags:      tags,
		Content:   content,
	}
	if err := SignEvent(event, privKey); err != nil {
		t.Fatalf("sign event: %v", err)
	}
	return event
}

// =====================
// Event Tests
// =====================

func TestEventSerialize(t *testing.T) {
	e := &Event{
		PubKey:    "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		CreatedAt: 1234567890,
		Kind:      1,
		Tags:      [][]string{},
		Content:   "hello world",
	}
	serialized, err := e.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	expected := `[0,"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",1234567890,1,[],"hello world"]`
	if string(serialized) != expected {
		t.Errorf("got  %s\nwant %s", serialized, expected)
	}
}

func TestEventSerializeSpecialChars(t *testing.T) {
	e := &Event{
		PubKey:    "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		CreatedAt: 1000,
		Kind:      1,
		Tags:      [][]string{},
		Content:   "hello\nworld\t\"test\"\\end <html>&amp;",
	}
	serialized, err := e.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	s := string(serialized)
	// SetEscapeHTML(false) means < > & should NOT be escaped
	if strings.Contains(s, `\u003c`) || strings.Contains(s, `\u003e`) || strings.Contains(s, `\u0026`) {
		t.Errorf("HTML chars should not be escaped: %s", s)
	}
	if !strings.Contains(s, `<html>`) {
		t.Errorf("expected literal <html> in output: %s", s)
	}
	if !strings.Contains(s, `&amp;`) {
		t.Errorf("expected literal &amp; in output: %s", s)
	}
}

func TestEventSerializeNilTags(t *testing.T) {
	e := &Event{
		PubKey:    "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		CreatedAt: 1000,
		Kind:      1,
		Tags:      nil,
		Content:   "test",
	}
	serialized, err := e.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if !strings.Contains(string(serialized), "[]") {
		t.Errorf("nil tags should serialize as []: %s", serialized)
	}
}

func TestComputeIDAndCheck(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "test content", nil)
	if !event.CheckID() {
		t.Error("CheckID should return true for correctly signed event")
	}

	// Tamper with content.
	event.Content = "tampered"
	if event.CheckID() {
		t.Error("CheckID should return false after tampering")
	}
}

func TestCheckSignature(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "signature test", nil)
	if !event.CheckSignature() {
		t.Error("CheckSignature should return true for correctly signed event")
	}

	// Tamper with signature.
	event.Sig = strings.Repeat("00", 64)
	if event.CheckSignature() {
		t.Error("CheckSignature should return false for tampered signature")
	}
}

func TestSignEvent(t *testing.T) {
	privKey, _ := btcec.NewPrivateKey()
	event := &Event{
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Tags:      [][]string{},
		Content:   "test",
	}
	if err := SignEvent(event, privKey); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if event.ID == "" || event.PubKey == "" || event.Sig == "" {
		t.Error("SignEvent should set ID, PubKey, and Sig")
	}
	if !event.CheckID() {
		t.Error("ID check should pass after signing")
	}
	if !event.CheckSignature() {
		t.Error("Signature check should pass after signing")
	}
}

// =====================
// Kind Tests
// =====================

func TestKindClassification(t *testing.T) {
	tests := []struct {
		kind         int
		isRegular    bool
		isReplaceable bool
		isEphemeral  bool
		isAddressable bool
	}{
		{0, false, true, false, false},
		{1, true, false, false, false},
		{2, true, false, false, false},
		{3, false, true, false, false},
		{4, true, false, false, false},
		{44, true, false, false, false},
		{45, false, false, false, false},
		{1000, true, false, false, false},
		{9999, true, false, false, false},
		{10000, false, true, false, false},
		{19999, false, true, false, false},
		{20000, false, false, true, false},
		{29999, false, false, true, false},
		{30000, false, false, false, true},
		{39999, false, false, false, true},
		{40000, false, false, false, false},
	}
	for _, tt := range tests {
		if got := IsRegular(tt.kind); got != tt.isRegular {
			t.Errorf("IsRegular(%d) = %v, want %v", tt.kind, got, tt.isRegular)
		}
		if got := IsReplaceable(tt.kind); got != tt.isReplaceable {
			t.Errorf("IsReplaceable(%d) = %v, want %v", tt.kind, got, tt.isReplaceable)
		}
		if got := IsEphemeral(tt.kind); got != tt.isEphemeral {
			t.Errorf("IsEphemeral(%d) = %v, want %v", tt.kind, got, tt.isEphemeral)
		}
		if got := IsAddressable(tt.kind); got != tt.isAddressable {
			t.Errorf("IsAddressable(%d) = %v, want %v", tt.kind, got, tt.isAddressable)
		}
	}
}

// =====================
// Filter Tests
// =====================

func TestFilterMatchByID(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "test", nil)
	f := &Filter{IDs: []string{event.ID}}
	if !f.Matches(event) {
		t.Error("should match by ID")
	}
	f2 := &Filter{IDs: []string{"nonexistent"}}
	if f2.Matches(event) {
		t.Error("should not match wrong ID")
	}
}

func TestFilterMatchByAuthor(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "test", nil)
	f := &Filter{Authors: []string{event.PubKey}}
	if !f.Matches(event) {
		t.Error("should match by author")
	}
}

func TestFilterMatchByKind(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "test", nil)
	f := &Filter{Kinds: []int{1}}
	if !f.Matches(event) {
		t.Error("should match by kind")
	}
	f2 := &Filter{Kinds: []int{2, 3}}
	if f2.Matches(event) {
		t.Error("should not match wrong kind")
	}
}

func TestFilterMatchBySince(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "test", nil)
	past := event.CreatedAt - 10
	f := &Filter{Since: &past}
	if !f.Matches(event) {
		t.Error("should match since < created_at")
	}
	future := event.CreatedAt + 10
	f2 := &Filter{Since: &future}
	if f2.Matches(event) {
		t.Error("should not match since > created_at")
	}
}

func TestFilterMatchByUntil(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "test", nil)
	future := event.CreatedAt + 10
	f := &Filter{Until: &future}
	if !f.Matches(event) {
		t.Error("should match until > created_at")
	}
	past := event.CreatedAt - 10
	f2 := &Filter{Until: &past}
	if f2.Matches(event) {
		t.Error("should not match until < created_at")
	}
}

func TestFilterMatchByTag(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "test", [][]string{
		{"e", "abc123"},
		{"p", "def456"},
	})
	f := &Filter{Tags: map[string][]string{"e": {"abc123"}}}
	if !f.Matches(event) {
		t.Error("should match by tag")
	}
	f2 := &Filter{Tags: map[string][]string{"e": {"wrong"}}}
	if f2.Matches(event) {
		t.Error("should not match wrong tag value")
	}
}

func TestFilterANDLogic(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "test", nil)
	// Both conditions match.
	f := &Filter{
		Kinds:   []int{1},
		Authors: []string{event.PubKey},
	}
	if !f.Matches(event) {
		t.Error("both conditions match, should match")
	}
	// One condition fails.
	f2 := &Filter{
		Kinds:   []int{2},
		Authors: []string{event.PubKey},
	}
	if f2.Matches(event) {
		t.Error("one condition fails, should not match")
	}
}

func TestFiltersORLogic(t *testing.T) {
	event, _ := makeSignedEvent(t, 1, "test", nil)
	filters := []Filter{
		{Kinds: []int{99}},       // doesn't match
		{Kinds: []int{1}},        // matches
	}
	if !FiltersMatch(filters, event) {
		t.Error("OR logic: should match if any filter matches")
	}
}

func TestFilterJSONRoundTrip(t *testing.T) {
	f := Filter{
		IDs:     []string{"abc"},
		Authors: []string{"def"},
		Kinds:   []int{1, 2},
		Tags:    map[string][]string{"e": {"123"}, "p": {"456"}},
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"#e"`) || !strings.Contains(s, `"#p"`) {
		t.Errorf("expected #e and #p in JSON: %s", s)
	}

	var f2 Filter
	if err := json.Unmarshal(data, &f2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(f2.Tags["e"]) != 1 || f2.Tags["e"][0] != "123" {
		t.Errorf("tag e not preserved: %v", f2.Tags)
	}
	if len(f2.Tags["p"]) != 1 || f2.Tags["p"][0] != "456" {
		t.Errorf("tag p not preserved: %v", f2.Tags)
	}
}

// =====================
// Store Tests
// =====================

func TestStoreRegularEvent(t *testing.T) {
	store := NewEventStore()
	event, _ := makeSignedEvent(t, 1, "regular event", nil)
	accepted, _ := store.StoreEvent(event)
	if !accepted {
		t.Error("regular event should be accepted")
	}
	results := store.Query([]Filter{{Kinds: []int{1}}})
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestStoreDuplicate(t *testing.T) {
	store := NewEventStore()
	event, _ := makeSignedEvent(t, 1, "dup test", nil)
	store.StoreEvent(event)
	accepted, msg := store.StoreEvent(event)
	if !accepted {
		t.Error("duplicate should still return accepted=true")
	}
	if msg != "duplicate: already have this event" {
		t.Errorf("expected duplicate message, got: %s", msg)
	}
}

func TestStoreReplaceableEvent(t *testing.T) {
	store := NewEventStore()
	privKey, _ := btcec.NewPrivateKey()

	// Kind 0 is replaceable.
	old := &Event{CreatedAt: 1000, Kind: 0, Tags: [][]string{}, Content: "old"}
	SignEvent(old, privKey)
	store.StoreEvent(old)

	newer := &Event{CreatedAt: 2000, Kind: 0, Tags: [][]string{}, Content: "new"}
	SignEvent(newer, privKey)
	accepted, _ := store.StoreEvent(newer)
	if !accepted {
		t.Error("newer replaceable should be accepted")
	}

	results := store.Query([]Filter{{Kinds: []int{0}, Authors: []string{newer.PubKey}}})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "new" {
		t.Errorf("expected newest event, got: %s", results[0].Content)
	}
}

func TestStoreReplaceableSameTimestamp(t *testing.T) {
	store := NewEventStore()
	privKey, _ := btcec.NewPrivateKey()

	e1 := &Event{CreatedAt: 1000, Kind: 0, Tags: [][]string{}, Content: "first"}
	SignEvent(e1, privKey)
	e2 := &Event{CreatedAt: 1000, Kind: 0, Tags: [][]string{}, Content: "second"}
	SignEvent(e2, privKey)

	store.StoreEvent(e1)
	store.StoreEvent(e2)

	results := store.Query([]Filter{{Kinds: []int{0}, Authors: []string{e1.PubKey}}})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// The one with the lower ID should be kept.
	var expected *Event
	if e1.ID < e2.ID {
		expected = e1
	} else {
		expected = e2
	}
	if results[0].ID != expected.ID {
		t.Errorf("expected event with lower ID %s, got %s", expected.ID, results[0].ID)
	}
}

func TestStoreAddressableEvent(t *testing.T) {
	store := NewEventStore()
	privKey, _ := btcec.NewPrivateKey()

	old := &Event{CreatedAt: 1000, Kind: 30000, Tags: [][]string{{"d", "profile"}}, Content: "old"}
	SignEvent(old, privKey)
	store.StoreEvent(old)

	newer := &Event{CreatedAt: 2000, Kind: 30000, Tags: [][]string{{"d", "profile"}}, Content: "new"}
	SignEvent(newer, privKey)
	store.StoreEvent(newer)

	results := store.Query([]Filter{{Kinds: []int{30000}}})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "new" {
		t.Errorf("expected newest addressable event, got: %s", results[0].Content)
	}
}

func TestStoreEphemeralNotStored(t *testing.T) {
	store := NewEventStore()
	event, _ := makeSignedEvent(t, 20001, "ephemeral", nil)
	accepted, msg := store.StoreEvent(event)
	if accepted {
		t.Error("ephemeral event should not be accepted for storage")
	}
	if msg != "ephemeral: event not stored" {
		t.Errorf("unexpected message: %s", msg)
	}
	results := store.Query([]Filter{{Kinds: []int{20001}}})
	if len(results) != 0 {
		t.Error("ephemeral events should not appear in query results")
	}
}

func TestStoreQueryLimitAndSort(t *testing.T) {
	store := NewEventStore()
	privKey, _ := btcec.NewPrivateKey()

	// Create events at different timestamps.
	for i := 0; i < 5; i++ {
		e := &Event{
			CreatedAt: int64(1000 + i),
			Kind:      1,
			Tags:      [][]string{},
			Content:   "",
		}
		SignEvent(e, privKey)
		store.StoreEvent(e)
	}

	limit := 3
	results := store.Query([]Filter{{Kinds: []int{1}, Limit: &limit}})
	if len(results) != 3 {
		t.Fatalf("expected 3 results with limit, got %d", len(results))
	}

	// Should be sorted by created_at descending.
	for i := 0; i < len(results)-1; i++ {
		if results[i].CreatedAt < results[i+1].CreatedAt {
			t.Error("results should be sorted by created_at descending")
		}
	}
}

// =====================
// Integration Tests
// =====================

func startTestRelay(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	relay := NewRelay()
	server := httptest.NewServer(relay)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	return server, wsURL
}

func TestIntegrationPublishAndSubscribe(t *testing.T) {
	server, wsURL := startTestRelay(t)
	defer server.Close()

	// Client 1 subscribes.
	c1, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect c1: %v", err)
	}
	defer c1.Close()

	ch := c1.Subscribe("sub1", Filter{Kinds: []int{1}})
	time.Sleep(50 * time.Millisecond) // Let subscription register.

	// Client 2 publishes.
	c2, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect c2: %v", err)
	}
	defer c2.Close()

	event, _ := makeSignedEvent(t, 1, "hello nostr", nil)
	accepted, _, err := c2.Publish(event)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !accepted {
		t.Error("event should be accepted")
	}

	// Client 1 should receive the event.
	select {
	case received := <-ch:
		if received.ID != event.ID {
			t.Errorf("received wrong event: got %s, want %s", received.ID, event.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event on subscription")
	}
}

func TestIntegrationStoredEventRetrieval(t *testing.T) {
	server, wsURL := startTestRelay(t)
	defer server.Close()

	// Publish an event.
	c1, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c1.Close()

	event, _ := makeSignedEvent(t, 1, "stored event", nil)
	c1.Publish(event)

	// New client subscribes and should receive the stored event.
	c2, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c2.Close()

	ch := c2.Subscribe("sub1", Filter{Kinds: []int{1}})
	select {
	case received := <-ch:
		if received.ID != event.ID {
			t.Errorf("got wrong stored event: %s", received.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stored event")
	}
}

func TestIntegrationCloseSubscription(t *testing.T) {
	server, wsURL := startTestRelay(t)
	defer server.Close()

	c1, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c1.Close()

	ch := c1.Subscribe("sub1", Filter{Kinds: []int{1}})
	time.Sleep(50 * time.Millisecond)

	c1.CloseSubscription("sub1")
	time.Sleep(50 * time.Millisecond)

	// Publish an event from another client.
	c2, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c2.Close()

	event, _ := makeSignedEvent(t, 1, "after close", nil)
	c2.Publish(event)

	// c1 should NOT receive since sub was closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("should not receive events after closing subscription")
		}
	case <-time.After(200 * time.Millisecond):
		// OK - no event received.
	}
}

func TestIntegrationOKMessages(t *testing.T) {
	server, wsURL := startTestRelay(t)
	defer server.Close()

	c, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	event, _ := makeSignedEvent(t, 1, "ok test", nil)
	accepted, _, err := c.Publish(event)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !accepted {
		t.Error("first publish should be accepted")
	}

	// Duplicate.
	accepted2, msg, err := c.Publish(event)
	if err != nil {
		t.Fatalf("publish dup: %v", err)
	}
	if !accepted2 {
		t.Error("duplicate should return accepted=true")
	}
	if !strings.Contains(msg, "duplicate") {
		t.Errorf("expected duplicate message, got: %s", msg)
	}
}

func TestIntegrationInvalidEvent(t *testing.T) {
	server, wsURL := startTestRelay(t)
	defer server.Close()

	c, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// Event with wrong ID.
	event, _ := makeSignedEvent(t, 1, "bad event", nil)
	event.ID = strings.Repeat("ab", 32) // Fake ID.
	accepted, msg, err := c.Publish(event)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if accepted {
		t.Error("invalid event should not be accepted")
	}
	if !strings.Contains(msg, "invalid") {
		t.Errorf("expected invalid message, got: %s", msg)
	}
}

func TestIntegrationEphemeralBroadcast(t *testing.T) {
	server, wsURL := startTestRelay(t)
	defer server.Close()

	// Client 1 subscribes to ephemeral events.
	c1, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c1.Close()

	ch := c1.Subscribe("sub1", Filter{Kinds: []int{20001}})
	time.Sleep(50 * time.Millisecond)

	// Client 2 publishes ephemeral event.
	c2, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c2.Close()

	event, _ := makeSignedEvent(t, 20001, "ephemeral msg", nil)
	accepted, _, err := c2.Publish(event)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !accepted {
		t.Error("ephemeral event should return accepted=true")
	}

	// Client 1 should receive it via broadcast.
	select {
	case received := <-ch:
		if received.ID != event.ID {
			t.Errorf("received wrong ephemeral event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ephemeral event broadcast")
	}

	// But it should NOT be in the store.
	c3, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c3.Close()

	ch2 := c3.Subscribe("sub2", Filter{Kinds: []int{20001}})
	select {
	case _, ok := <-ch2:
		if ok {
			t.Error("ephemeral event should not be in store")
		}
	case <-time.After(200 * time.Millisecond):
		// OK - not in store.
	}
}

func TestIntegrationReplaceableViaRelay(t *testing.T) {
	server, wsURL := startTestRelay(t)
	defer server.Close()

	c, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	privKey, _ := btcec.NewPrivateKey()

	// Publish older kind-0.
	e1 := &Event{CreatedAt: 1000, Kind: 0, Tags: [][]string{}, Content: "old profile"}
	SignEvent(e1, privKey)
	c.Publish(e1)

	// Publish newer kind-0.
	e2 := &Event{CreatedAt: 2000, Kind: 0, Tags: [][]string{}, Content: "new profile"}
	SignEvent(e2, privKey)
	c.Publish(e2)

	// New client queries should only get the latest.
	c2, err := Connect(wsURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c2.Close()

	ch := c2.Subscribe("sub1", Filter{Kinds: []int{0}, Authors: []string{e2.PubKey}})
	select {
	case received := <-ch:
		if received.Content != "new profile" {
			t.Errorf("expected latest replaceable, got: %s", received.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	// Make sure only one result.
	select {
	case ev := <-ch:
		t.Errorf("should only get one replaceable event, got extra: %s", ev.Content)
	case <-time.After(200 * time.Millisecond):
		// OK.
	}
}
