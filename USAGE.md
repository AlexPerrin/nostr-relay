# Usage

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

| File | Description |
|------|-------------|
| `event.go` | Event type, serialization, ID computation, BIP-340 Schnorr signing/verification, kind classification |
| `filter.go` | Subscription filter type with tag-key JSON marshaling and event matching |
| `store.go` | Thread-safe in-memory event store with NIP-01 replacement rules |
| `relay.go` | WebSocket relay server handling EVENT, REQ, and CLOSE messages |
| `client.go` | WebSocket client with publish, subscribe, and close operations |
| `main.go` | Entry point, starts the relay on `:9090` |
| `nostr_test.go` | Unit and integration tests |

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
