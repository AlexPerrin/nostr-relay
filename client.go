package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// OKResponse represents the relay's response to an EVENT message.
type OKResponse struct {
	EventID  string
	Accepted bool
	Message  string
}

// Client is a NIP-01 Nostr client.
type Client struct {
	ws   *websocket.Conn
	wmu  sync.Mutex // protects writes to ws
	done chan struct{}

	subMu sync.RWMutex
	subs  map[string]chan *Event

	okMu sync.Mutex
	okCh chan OKResponse
}

// Connect dials a WebSocket relay and starts the read loop.
func Connect(url string) (*Client, error) {
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	c := &Client{
		ws:   ws,
		done: make(chan struct{}),
		subs: make(map[string]chan *Event),
		okCh: make(chan OKResponse, 16),
	}

	go c.readLoop()
	return c, nil
}

// Publish sends an EVENT message and waits for the OK response.
func (c *Client) Publish(event *Event) (bool, string, error) {
	msg := []interface{}{"EVENT", event}
	c.wmu.Lock()
	err := c.ws.WriteJSON(msg)
	c.wmu.Unlock()
	if err != nil {
		return false, "", fmt.Errorf("write: %w", err)
	}

	select {
	case ok := <-c.okCh:
		return ok.Accepted, ok.Message, nil
	case <-time.After(5 * time.Second):
		return false, "", fmt.Errorf("timeout waiting for OK")
	case <-c.done:
		return false, "", fmt.Errorf("connection closed")
	}
}

// Subscribe sends a REQ message and returns a channel for receiving events.
func (c *Client) Subscribe(id string, filters ...Filter) chan *Event {
	ch := make(chan *Event, 64)

	c.subMu.Lock()
	c.subs[id] = ch
	c.subMu.Unlock()

	msg := make([]interface{}, 0, 2+len(filters))
	msg = append(msg, "REQ", id)
	for _, f := range filters {
		msg = append(msg, f)
	}

	c.wmu.Lock()
	c.ws.WriteJSON(msg)
	c.wmu.Unlock()

	return ch
}

// CloseSubscription sends a CLOSE message and cleans up the channel.
func (c *Client) CloseSubscription(id string) {
	c.wmu.Lock()
	c.ws.WriteJSON([]interface{}{"CLOSE", id})
	c.wmu.Unlock()

	c.subMu.Lock()
	if ch, ok := c.subs[id]; ok {
		close(ch)
		delete(c.subs, id)
	}
	c.subMu.Unlock()
}

// Close closes the WebSocket connection.
func (c *Client) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return c.ws.Close()
}

func (c *Client) readLoop() {
	defer func() {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
	}()

	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			return
		}

		var raw []json.RawMessage
		if err := json.Unmarshal(message, &raw); err != nil {
			continue
		}
		if len(raw) < 2 {
			continue
		}

		var msgType string
		if err := json.Unmarshal(raw[0], &msgType); err != nil {
			continue
		}

		switch msgType {
		case "EVENT":
			c.handleEventMsg(raw)
		case "OK":
			c.handleOKMsg(raw)
		case "EOSE":
			// Could be used for synchronization; ignore for now.
		case "NOTICE":
			// Log or ignore.
		case "CLOSED":
			c.handleClosedMsg(raw)
		}
	}
}

func (c *Client) handleEventMsg(raw []json.RawMessage) {
	if len(raw) < 3 {
		return
	}
	var subID string
	if err := json.Unmarshal(raw[1], &subID); err != nil {
		return
	}
	var event Event
	if err := json.Unmarshal(raw[2], &event); err != nil {
		return
	}

	c.subMu.RLock()
	ch, ok := c.subs[subID]
	c.subMu.RUnlock()
	if ok {
		select {
		case ch <- &event:
		default:
			// Channel full, drop event.
		}
	}
}

func (c *Client) handleOKMsg(raw []json.RawMessage) {
	if len(raw) < 4 {
		return
	}
	var eventID string
	var accepted bool
	var message string
	if err := json.Unmarshal(raw[1], &eventID); err != nil {
		return
	}
	if err := json.Unmarshal(raw[2], &accepted); err != nil {
		return
	}
	if err := json.Unmarshal(raw[3], &message); err != nil {
		return
	}

	select {
	case c.okCh <- OKResponse{EventID: eventID, Accepted: accepted, Message: message}:
	default:
	}
}

func (c *Client) handleClosedMsg(raw []json.RawMessage) {
	if len(raw) < 3 {
		return
	}
	var subID string
	if err := json.Unmarshal(raw[1], &subID); err != nil {
		return
	}

	c.subMu.Lock()
	if ch, ok := c.subs[subID]; ok {
		close(ch)
		delete(c.subs, subID)
	}
	c.subMu.Unlock()
}
