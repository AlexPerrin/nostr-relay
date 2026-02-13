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

// Serialize produces the canonical JSON array used for ID computation:
// [0, pubkey, created_at, kind, tags, content]
func (e *Event) Serialize() ([]byte, error) {
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
	// json.Encoder appends a newline; strip it
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
}

// ComputeID computes the SHA-256 hash of the serialized event and returns it as lowercase hex.
func (e *Event) ComputeID() (string, error) {
	serialized, err := e.Serialize()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(serialized)
	return hex.EncodeToString(h[:]), nil
}

// CheckID returns true if the event's ID matches the computed ID.
func (e *Event) CheckID() bool {
	computed, err := e.ComputeID()
	if err != nil {
		return false
	}
	return e.ID == computed
}

// CheckSignature verifies the Schnorr signature against the event ID and pubkey.
func (e *Event) CheckSignature() bool {
	pubBytes, err := hex.DecodeString(e.PubKey)
	if err != nil || len(pubBytes) != 32 {
		return false
	}
	pk, err := schnorr.ParsePubKey(pubBytes)
	if err != nil {
		return false
	}

	sigBytes, err := hex.DecodeString(e.Sig)
	if err != nil || len(sigBytes) != 64 {
		return false
	}
	sig, err := schnorr.ParseSignature(sigBytes)
	if err != nil {
		return false
	}

	idBytes, err := hex.DecodeString(e.ID)
	if err != nil || len(idBytes) != 32 {
		return false
	}

	return sig.Verify(idBytes, pk)
}

// SignEvent sets the pubkey, computes the ID, and signs the event with the given private key.
func SignEvent(e *Event, privKey *btcec.PrivateKey) error {
	pubBytes := privKey.PubKey().SerializeCompressed()
	// x-only pubkey is the last 32 bytes of compressed (skip the prefix byte)
	e.PubKey = hex.EncodeToString(pubBytes[1:])

	id, err := e.ComputeID()
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

// Kind classification helpers per NIP-01.

func IsRegular(kind int) bool {
	return kind == 1 || kind == 2 || (kind >= 4 && kind < 45) || (kind >= 1000 && kind < 10000)
}

func IsReplaceable(kind int) bool {
	return kind == 0 || kind == 3 || (kind >= 10000 && kind < 20000)
}

func IsEphemeral(kind int) bool {
	return kind >= 20000 && kind < 30000
}

func IsAddressable(kind int) bool {
	return kind >= 30000 && kind < 40000
}
