package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// OKResponse is the relay's reply to an EVENT message.
type OKResponse struct {
	EventID  string
	Accepted bool
	Message  string
}

// Client is a NIP-01 WebSocket client.
type Client struct {
	ws  *websocket.Conn
	wmu sync.Mutex

	done chan struct{}

	subMu   sync.RWMutex
	subs    map[string]chan *Event
	eoseFns map[string]func()

	okCh chan OKResponse
}

// Connect dials a relay and starts the read loop.
func Connect(url string) (*Client, error) {
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	c := &Client{
		ws:      ws,
		done:    make(chan struct{}),
		subs:    make(map[string]chan *Event),
		eoseFns: make(map[string]func()),
		okCh:    make(chan OKResponse, 16),
	}
	go c.readLoop()
	return c, nil
}

// Done returns a channel closed when the connection is lost.
func (c *Client) Done() <-chan struct{} { return c.done }

// Publish sends EVENT and waits for the relay's OK response.
func (c *Client) Publish(event *Event) (bool, string, error) {
	c.wmu.Lock()
	err := c.ws.WriteJSON([]interface{}{"EVENT", event})
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

// Subscribe sends REQ and returns a channel that delivers matching events.
func (c *Client) Subscribe(id string, filters ...Filter) <-chan *Event {
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
	c.ws.WriteJSON(msg) //nolint:errcheck
	c.wmu.Unlock()
	return ch
}

// OnEOSE registers a callback for the end-of-stored-events marker.
func (c *Client) OnEOSE(subID string, fn func()) {
	c.subMu.Lock()
	c.eoseFns[subID] = fn
	c.subMu.Unlock()
}

// CloseSubscription sends CLOSE and tears down the event channel.
func (c *Client) CloseSubscription(id string) {
	c.wmu.Lock()
	c.ws.WriteJSON([]interface{}{"CLOSE", id}) //nolint:errcheck
	c.wmu.Unlock()

	c.subMu.Lock()
	if ch, ok := c.subs[id]; ok {
		close(ch)
		delete(c.subs, id)
	}
	delete(c.eoseFns, id)
	c.subMu.Unlock()
}

// Close shuts down the connection.
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
		// Close all subscription channels so waitForEventCmd unblocks.
		c.subMu.Lock()
		for id, ch := range c.subs {
			close(ch)
			delete(c.subs, id)
		}
		c.subMu.Unlock()
	}()

	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var raw []json.RawMessage
		if err := json.Unmarshal(msg, &raw); err != nil || len(raw) < 2 {
			continue
		}
		var typ string
		if err := json.Unmarshal(raw[0], &typ); err != nil {
			continue
		}
		switch typ {
		case "EVENT":
			c.handleEvent(raw)
		case "OK":
			c.handleOK(raw)
		case "EOSE":
			c.handleEOSE(raw)
		case "CLOSED":
			c.handleClosed(raw)
		}
	}
}

func (c *Client) handleEvent(raw []json.RawMessage) {
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
	ch := c.subs[subID]
	c.subMu.RUnlock()
	if ch != nil {
		select {
		case ch <- &event:
		default:
		}
	}
}

func (c *Client) handleOK(raw []json.RawMessage) {
	if len(raw) < 4 {
		return
	}
	var eventID string
	var accepted bool
	var message string
	json.Unmarshal(raw[1], &eventID)   //nolint:errcheck
	json.Unmarshal(raw[2], &accepted)  //nolint:errcheck
	json.Unmarshal(raw[3], &message)   //nolint:errcheck
	select {
	case c.okCh <- OKResponse{EventID: eventID, Accepted: accepted, Message: message}:
	default:
	}
}

func (c *Client) handleEOSE(raw []json.RawMessage) {
	if len(raw) < 2 {
		return
	}
	var subID string
	if err := json.Unmarshal(raw[1], &subID); err != nil {
		return
	}
	c.subMu.RLock()
	fn := c.eoseFns[subID]
	c.subMu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (c *Client) handleClosed(raw []json.RawMessage) {
	if len(raw) < 2 {
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
	delete(c.eoseFns, subID)
	c.subMu.Unlock()
}
