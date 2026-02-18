package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Relay is a NIP-01 compliant Nostr relay.
type Relay struct {
	store    *EventStore
	upgrader websocket.Upgrader
	mu       sync.RWMutex
	conns    map[*Connection]struct{}
}

// Connection represents a single WebSocket connection to the relay.
type Connection struct {
	ws    *websocket.Conn
	wmu   sync.Mutex // protects writes to ws
	relay *Relay

	subMu sync.RWMutex
	subs  map[string][]Filter // subscription_id -> filters
}

// NewRelay creates a new Relay.
func NewRelay() *Relay {
	return &Relay{
		store: NewEventStore(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		conns: make(map[*Connection]struct{}),
	}
}

// ServeHTTP upgrades HTTP connections to WebSocket and starts handling messages.
func (r *Relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ws, err := r.upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}

	conn := &Connection{
		ws:    ws,
		relay: r,
		subs:  make(map[string][]Filter),
	}

	r.mu.Lock()
	r.conns[conn] = struct{}{}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.conns, conn)
		r.mu.Unlock()
		ws.Close()
	}()

	conn.readLoop()
}

func (c *Connection) readLoop() {
	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			return
		}

		var raw []json.RawMessage
		if err := json.Unmarshal(message, &raw); err != nil {
			c.sendNotice("error: invalid message format")
			continue
		}
		if len(raw) < 2 {
			c.sendNotice("error: message too short")
			continue
		}

		var msgType string
		if err := json.Unmarshal(raw[0], &msgType); err != nil {
			c.sendNotice("error: invalid message type")
			continue
		}

		switch msgType {
		case "EVENT":
			c.handleEvent(raw)
		case "REQ":
			c.handleReq(raw)
		case "CLOSE":
			c.handleClose(raw)
		default:
			c.sendNotice("error: unknown message type: " + msgType)
		}
	}
}

func (c *Connection) handleEvent(raw []json.RawMessage) {
	if len(raw) < 2 {
		c.sendNotice("error: EVENT message missing event data")
		return
	}

	var event Event
	if err := json.Unmarshal(raw[1], &event); err != nil {
		c.sendNotice("error: invalid event JSON")
		return
	}

	// Ensure tags is never nil for JSON serialization.
	if event.Tags == nil {
		event.Tags = [][]string{}
	}

	// Validate ID.
	if !event.CheckID() {
		c.sendOK(event.ID, false, "invalid: event id does not match")
		return
	}

	// Validate signature.
	if !event.CheckSignature() {
		c.sendOK(event.ID, false, "invalid: signature verification failed")
		return
	}

	// For ephemeral events, broadcast but don't store.
	if IsEphemeral(event.Kind) {
		c.sendOK(event.ID, true, "")
		c.relay.broadcast(&event, c)
		return
	}

	accepted, msg := c.relay.store.StoreEvent(&event)
	c.sendOK(event.ID, accepted, msg)

	if accepted && msg != "duplicate: already have this event" {
		c.relay.broadcast(&event, c)
	}
}

func (c *Connection) handleReq(raw []json.RawMessage) {
	if len(raw) < 3 {
		c.sendNotice("error: REQ message too short")
		return
	}

	var subID string
	if err := json.Unmarshal(raw[1], &subID); err != nil {
		c.sendNotice("error: invalid subscription ID")
		return
	}

	if subID == "" || len(subID) > 64 {
		c.sendClosed(subID, "error: subscription ID must be non-empty and max 64 chars")
		return
	}

	var filters []Filter
	for i := 2; i < len(raw); i++ {
		var f Filter
		if err := json.Unmarshal(raw[i], &f); err != nil {
			c.sendClosed(subID, "error: invalid filter")
			return
		}
		filters = append(filters, f)
	}

	// Register subscription (replaces existing with same ID).
	c.subMu.Lock()
	c.subs[subID] = filters
	c.subMu.Unlock()

	// Query stored events.
	events := c.relay.store.Query(filters)
	for _, event := range events {
		c.sendEvent(subID, event)
	}
	c.sendEOSE(subID)
}

func (c *Connection) handleClose(raw []json.RawMessage) {
	if len(raw) < 2 {
		c.sendNotice("error: CLOSE message too short")
		return
	}

	var subID string
	if err := json.Unmarshal(raw[1], &subID); err != nil {
		c.sendNotice("error: invalid subscription ID")
		return
	}

	c.subMu.Lock()
	delete(c.subs, subID)
	c.subMu.Unlock()
}

func (r *Relay) broadcast(event *Event, sender *Connection) {
	r.mu.RLock()
	conns := make([]*Connection, 0, len(r.conns))
	for conn := range r.conns {
		conns = append(conns, conn)
	}
	r.mu.RUnlock()

	for _, conn := range conns {
		conn.subMu.RLock()
		for subID, filters := range conn.subs {
			if FiltersMatch(filters, event) {
				conn.sendEvent(subID, event)
			}
		}
		conn.subMu.RUnlock()
	}
}

// Message sending helpers.

func (c *Connection) sendJSON(v interface{}) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.ws.WriteJSON(v)
}

func (c *Connection) sendOK(eventID string, ok bool, message string) {
	c.sendJSON([]interface{}{"OK", eventID, ok, message})
}

func (c *Connection) sendEvent(subID string, event *Event) {
	c.sendJSON([]interface{}{"EVENT", subID, event})
}

func (c *Connection) sendEOSE(subID string) {
	c.sendJSON([]interface{}{"EOSE", subID})
}

func (c *Connection) sendNotice(message string) {
	c.sendJSON([]interface{}{"NOTICE", message})
}

func (c *Connection) sendClosed(subID, message string) {
	c.sendJSON([]interface{}{"CLOSED", subID, message})
}
