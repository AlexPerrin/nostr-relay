package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

// Event represents a NIP-01 Nostr event.
type Event struct {
	ID        string     `json:"id"`
	PubKey    string     `json:"pubkey"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      [][]string `json:"tags"`
	Content   string     `json:"content"`
	Sig       string     `json:"sig"`
}

func (e *Event) serialize() ([]byte, error) {
	tags := e.Tags
	if tags == nil {
		tags = [][]string{}
	}
	arr := []interface{}{0, e.PubKey, e.CreatedAt, e.Kind, tags, e.Content}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(arr); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
}

func (e *Event) computeID() (string, error) {
	s, err := e.serialize()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(s)
	return hex.EncodeToString(h[:]), nil
}

// SignEvent sets PubKey, computes ID, and signs the event.
func SignEvent(e *Event, privKey *btcec.PrivateKey) error {
	pubBytes := privKey.PubKey().SerializeCompressed()
	e.PubKey = hex.EncodeToString(pubBytes[1:]) // x-only pubkey

	id, err := e.computeID()
	if err != nil {
		return fmt.Errorf("compute ID: %w", err)
	}
	e.ID = id

	idBytes, err := hex.DecodeString(e.ID)
	if err != nil {
		return fmt.Errorf("decode ID: %w", err)
	}
	sig, err := schnorr.Sign(privKey, idBytes)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	e.Sig = hex.EncodeToString(sig.Serialize())
	return nil
}

// Filter is a NIP-01 subscription filter with custom JSON for #<tag> keys.
type Filter struct {
	IDs     []string            `json:"ids,omitempty"`
	Authors []string            `json:"authors,omitempty"`
	Kinds   []int               `json:"kinds,omitempty"`
	Since   *int64              `json:"since,omitempty"`
	Until   *int64              `json:"until,omitempty"`
	Limit   *int                `json:"limit,omitempty"`
	Tags    map[string][]string `json:"-"`
}

func (f Filter) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{})
	if len(f.IDs) > 0 {
		m["ids"] = f.IDs
	}
	if len(f.Authors) > 0 {
		m["authors"] = f.Authors
	}
	if len(f.Kinds) > 0 {
		m["kinds"] = f.Kinds
	}
	if f.Since != nil {
		m["since"] = *f.Since
	}
	if f.Until != nil {
		m["until"] = *f.Until
	}
	if f.Limit != nil {
		m["limit"] = *f.Limit
	}
	for k, v := range f.Tags {
		m["#"+k] = v
	}
	return json.Marshal(m)
}
