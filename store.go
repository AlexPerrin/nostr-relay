package main

import (
	"fmt"
	"sort"
	"sync"
)

// EventStore is a thread-safe in-memory store for Nostr events.
type EventStore struct {
	mu           sync.RWMutex
	events       map[string]*Event  // id -> event
	replaceIndex map[string]string  // "pubkey:kind" -> event id (for replaceable)
	addressIndex map[string]string  // "kind:pubkey:dtag" -> event id (for addressable)
}

// NewEventStore creates a new empty EventStore.
func NewEventStore() *EventStore {
	return &EventStore{
		events:       make(map[string]*Event),
		replaceIndex: make(map[string]string),
		addressIndex: make(map[string]string),
	}
}

// StoreEvent stores an event following NIP-01 kind rules.
// Returns (accepted bool, message string).
func (s *EventStore) StoreEvent(event *Event) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ephemeral events are never stored.
	if IsEphemeral(event.Kind) {
		return false, "ephemeral: event not stored"
	}

	// Reject duplicates.
	if _, exists := s.events[event.ID]; exists {
		return true, "duplicate: already have this event"
	}

	if IsReplaceable(event.Kind) {
		return s.storeReplaceable(event)
	}

	if IsAddressable(event.Kind) {
		return s.storeAddressable(event)
	}

	// Regular event: just store it.
	s.events[event.ID] = event
	return true, ""
}

func (s *EventStore) storeReplaceable(event *Event) (bool, string) {
	key := fmt.Sprintf("%s:%d", event.PubKey, event.Kind)
	if existingID, ok := s.replaceIndex[key]; ok {
		existing := s.events[existingID]
		if event.CreatedAt < existing.CreatedAt {
			return false, "replaced: event is older"
		}
		if event.CreatedAt == existing.CreatedAt && event.ID >= existing.ID {
			return false, "replaced: event has same timestamp but higher id"
		}
		// New event wins: remove old.
		delete(s.events, existingID)
	}
	s.events[event.ID] = event
	s.replaceIndex[key] = event.ID
	return true, ""
}

func (s *EventStore) storeAddressable(event *Event) (bool, string) {
	dtag := ""
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			dtag = tag[1]
			break
		}
	}
	key := fmt.Sprintf("%d:%s:%s", event.Kind, event.PubKey, dtag)
	if existingID, ok := s.addressIndex[key]; ok {
		existing := s.events[existingID]
		if event.CreatedAt < existing.CreatedAt {
			return false, "replaced: event is older"
		}
		if event.CreatedAt == existing.CreatedAt && event.ID >= existing.ID {
			return false, "replaced: event has same timestamp but higher id"
		}
		// New event wins: remove old.
		delete(s.events, existingID)
	}
	s.events[event.ID] = event
	s.addressIndex[key] = event.ID
	return true, ""
}

// Query returns events matching any of the given filters, sorted by
// created_at desc then id asc, with the minimum limit applied.
func (s *EventStore) Query(filters []Filter) []*Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*Event
	seen := make(map[string]bool)

	for _, event := range s.events {
		if seen[event.ID] {
			continue
		}
		if FiltersMatch(filters, event) {
			matched = append(matched, event)
			seen[event.ID] = true
		}
	}

	// Sort: created_at descending, then id ascending for ties.
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].CreatedAt != matched[j].CreatedAt {
			return matched[i].CreatedAt > matched[j].CreatedAt
		}
		return matched[i].ID < matched[j].ID
	})

	// Apply the smallest limit from any filter that specifies one.
	var limit int
	for _, f := range filters {
		if f.Limit != nil {
			if limit == 0 || *f.Limit < limit {
				limit = *f.Limit
			}
		}
	}
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}

	return matched
}
