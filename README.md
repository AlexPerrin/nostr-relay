# nostr-relay

### [Nostr - Notes and Other Stuff Transmitted by Relays](https://github.com/nostr-protocol/nostr)

*"The simplest open protocol that is able to create a censorship-resistant global "social" network once and for all."*[^1]

*"Relays can be hosted by anyone and have any rule or internal policy they want. The fact that the protocol is open makes it so that, as long as there is any relay willing to host someone, they can still publish their stuff for their followers, and the followers can find their stuff in that relay."*[^1]

### [NIPs - Nostr Implementation Possibilities](https://github.com/nostr-protocol/nips)

- [x] [NIP-01](https://github.com/nostr-protocol/nips/blob/master/01.md) Basic protocol flow description
- [ ] [NIP-02](https://github.com/nostr-protocol/nips/blob/master/02.md) Follow List
- [ ] [NIP-04](https://github.com/nostr-protocol/nips/blob/master/04.md) Encrypted Direct Message
- [ ] [NIP-09](https://github.com/nostr-protocol/nips/blob/master/09.md) Event Deletion Request
- [ ] [NIP-10](https://github.com/nostr-protocol/nips/blob/master/10.md) Text Notes and Threads
- [ ] [NIP-11](https://github.com/nostr-protocol/nips/blob/master/11.md) Relay Information Document
- [ ] [NIP-14](https://github.com/nostr-protocol/nips/blob/master/14.md) Subject tag in Text events

### Next Steps

- Persistent event store database (PostgreSQL)
- Continue implementing additional NIPs.

### Goals & Motivations

- Implement a Nostr relay in Go.
- Improve backend development skills with Go.
- Build an understanding of the Nostr protocol.
- Exploring ideas for developing a social empathy network.

## CLI Client

`nostr-cli` is an interactive terminal UI for generating keys, publishing events, and watching the feed on any NIP-01 relay. It is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

### Build

```sh
go build -o nostr-cli ./cmd/nostr-cli
```

### Run

```sh
./nostr-cli
```

### Screens

Navigate between screens with `1`, `2`, `3`. Quit with `q`.

#### `[1] Profile`

Configure your identity and relay endpoint.

| Key | Action |
|-----|--------|
| `g` | Generate a new secp256k1 keypair |
| `e` | Enter edit mode (relay URL + private key fields) |
| `Tab` | Move between fields |
| `Enter` | Save (derives and displays the public key) |
| `Esc` | Cancel editing |

After pressing `g`, the full private key hex is displayed on screen — copy and store it somewhere safe before navigating away.

To import an existing key, press `e`, tab to the *Private Key* field, paste your 64-char hex private key, and press `Enter`.

**Example — generate a key and set a custom relay:**

```
[1] Profile screen

  Relay URL
  > ws://localhost:9090

  Private Key (hex)
  > ••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••

  ⚠  Back up your private key — it cannot be recovered:
  9a44527cc452c7a0a5fb597b3dc26d8c87c5c3cab070b9af3e3398923529bc55

  Public Key
  4f9a1c2d8e3b7a0f5c6d2e1a8b4f3c7d9e2a1b5f8c4d6e3a7b2f1c9d5e8a3b6

  key generated ✓
```

#### `[2] Feed`

Connects to the relay on first visit and streams kind:1 (text note) events in a scrollable viewport.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Scroll through events |
| `r` | Reconnect (useful after changing the relay in Profile) |

Stored events load immediately on connection; a dotted divider marks the boundary where live events begin.

#### `[3] Post`

Compose and publish a kind:1 text note. Requires a key to be set in Profile first.

| Key | Action |
|-----|--------|
| `i` | Focus the text area and start writing |
| `ctrl+s` | Sign and publish the event |
| `Esc` | Unfocus the text area |

### Testing the client

Unit tests (no relay required) cover key generation, key parsing, event ID computation, and Schnorr signing. Integration tests require the relay to be running at `ws://localhost:9090/` and are skipped automatically if it is not reachable.

```sh
# Unit tests only
go test ./cmd/nostr-cli/ -run 'TestGenerateKey|TestParsePrivKey|TestSignEvent'

# All tests (relay must be running)
go test ./cmd/nostr-cli/ -v
```

## Prerequisites

- [Go](https://go.dev/dl/) 1.21 or later

## Build

```sh
go build -o nostr-relay .
```

## Run

Start the relay on the default port (9090):

```sh
./nostr-relay
```

Or run directly without building:

```sh
go run .
```

The relay accepts WebSocket connections at `ws://localhost:9090/`.

## Kafka Event Streaming

The relay supports multi-instance event distribution via Kafka. When enabled, every event submitted to one relay instance is propagated to all others.

### Configuration

Kafka is configured via environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `KAFKA_BROKERS` | Yes (to enable) | *(none — Kafka disabled)* | Comma-separated broker addresses |
| `KAFKA_TOPIC` | No | `nostr-events` | Kafka topic for events |
| `KAFKA_CONSUMER_GROUP` | No | `nostr-relay-<hostname>` | Consumer group ID |

The default consumer group includes the hostname so that each relay instance receives all events (fan-out mode).

### Running with Kafka

```sh
KAFKA_BROKERS=localhost:9092 ./nostr-relay
```

With custom settings:

```sh
KAFKA_BROKERS=broker1:9092,broker2:9092 \
KAFKA_TOPIC=my-events \
KAFKA_CONSUMER_GROUP=my-group \
./nostr-relay
```

Without `KAFKA_BROKERS` set, the relay runs in standalone mode with no Kafka dependency.

## Test

Run all tests:

```sh
go test ./...
```

Verbose output:

```sh
go test -v ./...
```

## Connecting

Any NIP-01 compatible Nostr client can connect to `ws://localhost:9090/`. You can also use command-line WebSocket tools for quick testing.

### websocat

Install [websocat](https://github.com/vi/websocat), then:

```sh
websocat ws://localhost:9090/
```

Once connected, send JSON messages per the NIP-01 protocol:

```json
["REQ", "my-sub", {"kinds": [1], "limit": 10}]
```

### Go client API

The repository includes a `Client` type (`client.go`) for interacting with any NIP-01 relay programmatically. All methods are safe for concurrent use.

#### Connect

`Connect(url string) (*Client, error)` dials a WebSocket relay and starts a background read loop that dispatches incoming messages to the appropriate channels.

```go
client, err := Connect("ws://localhost:9090/")
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

#### Publish

`Publish(event *Event) (accepted bool, message string, err error)` sends an `["EVENT", ...]` message to the relay and blocks until the relay responds with an `OK` message (up to a 5-second timeout).

The event must be signed before publishing. Use `SignEvent` from `event.go` to set the pubkey, compute the ID, and produce a BIP-340 Schnorr signature.

```go
privKey, _ := btcec.NewPrivateKey()

event := &Event{
    CreatedAt: time.Now().Unix(),
    Kind:      1,
    Tags:      [][]string{},
    Content:   "Hello, Nostr!",
}
SignEvent(event, privKey)

accepted, msg, err := client.Publish(event)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("accepted=%v msg=%q\n", accepted, msg)
// accepted=true msg=""
```

The relay returns `accepted=true` for valid events and duplicates (duplicates include the message `"duplicate: already have this event"`). It returns `accepted=false` with an `"invalid: ..."` message when the ID or signature check fails.

#### Subscribe

`Subscribe(id string, filters ...Filter) chan *Event` sends a `["REQ", ...]` message to the relay and returns a buffered channel (capacity 64) that receives matching events.

The relay first sends all stored events that match the filters, then continues sending new events in real time as they arrive. The subscription ID is an arbitrary string (non-empty, max 64 chars) that you choose.

```go
// Subscribe to kind-1 text notes from a specific author.
ch := client.Subscribe("my-sub", Filter{
    Kinds:   []int{1},
    Authors: []string{"abcdef0123456789..."},
})

// Read events as they arrive.
for event := range ch {
    fmt.Printf("%s: %s\n", event.ID[:8], event.Content)
}
```

Multiple filters can be passed to create an OR query (an event matching any filter is delivered):

```go
ch := client.Subscribe("multi",
    Filter{Kinds: []int{0}},   // user metadata
    Filter{Kinds: []int{1}},   // text notes
)
```

Filters support these fields:

| Field | Type | Description |
|-------|------|-------------|
| `IDs` | `[]string` | Match specific event IDs |
| `Authors` | `[]string` | Match events by pubkey |
| `Kinds` | `[]int` | Match event kinds |
| `Since` | `*int64` | Events with `created_at >= since` |
| `Until` | `*int64` | Events with `created_at <= until` |
| `Limit` | `*int` | Max events returned from stored history |
| `Tags` | `map[string][]string` | Tag filters, e.g. `{"e": ["<id>"]}` for `#e` |

All specified fields must match (AND logic). Across multiple filters, any match suffices (OR logic).

#### CloseSubscription

`CloseSubscription(id string)` sends a `["CLOSE", ...]` message to the relay, closes the event channel, and removes the subscription. After calling this, the channel returned by `Subscribe` is closed and a `range` loop over it will exit.

```go
client.CloseSubscription("my-sub")
```

#### Close

`Close() error` shuts down the WebSocket connection and stops the background read loop. Any active subscription channels should be considered invalid after this call.

```go
client.Close()
```

#### Full example

```go
package main

import (
    "fmt"
    "time"

    "github.com/btcsuite/btcd/btcec/v2"
)

func main() {
    client, err := Connect("ws://localhost:9090/")
    if err != nil {
        panic(err)
    }
    defer client.Close()

    // Subscribe before publishing so we receive our own event.
    ch := client.Subscribe("my-sub", Filter{Kinds: []int{1}})

    // Create, sign, and publish an event.
    privKey, _ := btcec.NewPrivateKey()
    event := &Event{
        CreatedAt: time.Now().Unix(),
        Kind:      1,
        Tags:      [][]string{},
        Content:   "Hello, Nostr!",
    }
    SignEvent(event, privKey)

    accepted, msg, err := client.Publish(event)
    fmt.Printf("accepted=%v msg=%q err=%v\n", accepted, msg, err)

    // Read one event then clean up.
    ev := <-ch
    fmt.Printf("received event %s: %s\n", ev.ID[:8], ev.Content)

    client.CloseSubscription("my-sub")
}
```

## Project Structure

### Relay (`/`)

| File | Description |
|------|-------------|
| `event.go` | Event type, serialization, ID computation, BIP-340 Schnorr signing/verification, kind classification |
| `filter.go` | Subscription filter type with tag-key JSON marshaling and event matching |
| `store.go` | Thread-safe in-memory event store with NIP-01 replacement rules |
| `relay.go` | WebSocket relay server handling EVENT, REQ, and CLOSE messages |
| `kafka.go` | Kafka event bus for multi-relay event distribution |
| `client.go` | WebSocket client with publish, subscribe, and close operations |
| `main.go` | Entry point, starts the relay on `:9090`, optional Kafka setup |
| `nostr_test.go` | Unit and integration tests |
| `kafka_test.go` | Kafka configuration and integration tests |

### CLI client (`cmd/nostr-cli/`)

| File | Description |
|------|-------------|
| `main.go` | Entry point, starts the Bubble Tea program |
| `model.go` | Root model, tab routing, shared message types and styles |
| `profile.go` | Profile screen — key generation, key import, relay URL |
| `feed.go` | Feed screen — live event subscription and scrollable viewport |
| `post.go` | Post screen — event composition and publishing |
| `event.go` | Event and Filter types, Schnorr signing (CLI-local copy) |
| `client.go` | WebSocket client (CLI-local copy) |
| `client_test.go` | Unit and integration tests for keygen, signing, and relay comms |

## NIP-01 Protocol Summary

### Client-to-relay messages

- `["EVENT", <event>]` -- publish an event
- `["REQ", <sub_id>, <filter>, ...]` -- subscribe to events matching filters
- `["CLOSE", <sub_id>]` -- close a subscription

### Relay-to-client messages

- `["EVENT", <sub_id>, <event>]` -- an event matching a subscription
- `["OK", <event_id>, <bool>, <message>]` -- acceptance/rejection of a published event
- `["EOSE", <sub_id>]` -- end of stored events for a subscription
- `["CLOSED", <sub_id>, <message>]` -- subscription closed by the relay
- `["NOTICE", <message>]` -- human-readable relay message

### Event kinds

| Range | Type | Behavior |
|-------|------|----------|
| 1, 2, 4-44, 1000-9999 | Regular | All events stored |
| 0, 3, 10000-19999 | Replaceable | Latest per `pubkey:kind` kept |
| 20000-29999 | Ephemeral | Broadcast only, never stored |
| 30000-39999 | Addressable | Latest per `kind:pubkey:d-tag` kept |

[^1]:https://github.com/nostr-protocol/nostr
