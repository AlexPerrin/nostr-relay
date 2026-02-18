package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

func TestKafkaConfigFromEnv_Disabled(t *testing.T) {
	os.Unsetenv("KAFKA_BROKERS")
	cfg := KafkaConfigFromEnv()
	if cfg.Enabled {
		t.Fatal("expected Kafka to be disabled when KAFKA_BROKERS is not set")
	}
}

func TestKafkaConfigFromEnv_Defaults(t *testing.T) {
	os.Setenv("KAFKA_BROKERS", "localhost:9092")
	defer os.Unsetenv("KAFKA_BROKERS")
	os.Unsetenv("KAFKA_TOPIC")
	os.Unsetenv("KAFKA_CONSUMER_GROUP")

	cfg := KafkaConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected Kafka to be enabled")
	}
	if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "localhost:9092" {
		t.Fatalf("unexpected brokers: %v", cfg.Brokers)
	}
	if cfg.Topic != "nostr-events" {
		t.Fatalf("unexpected topic: %s", cfg.Topic)
	}
	hostname, _ := os.Hostname()
	expected := "nostr-relay-" + hostname
	if cfg.ConsumerGroup != expected {
		t.Fatalf("expected consumer group %q, got %q", expected, cfg.ConsumerGroup)
	}
}

func TestKafkaConfigFromEnv_CustomValues(t *testing.T) {
	os.Setenv("KAFKA_BROKERS", "broker1:9092,broker2:9092")
	os.Setenv("KAFKA_TOPIC", "custom-topic")
	os.Setenv("KAFKA_CONSUMER_GROUP", "custom-group")
	defer func() {
		os.Unsetenv("KAFKA_BROKERS")
		os.Unsetenv("KAFKA_TOPIC")
		os.Unsetenv("KAFKA_CONSUMER_GROUP")
	}()

	cfg := KafkaConfigFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected Kafka to be enabled")
	}
	if len(cfg.Brokers) != 2 {
		t.Fatalf("expected 2 brokers, got %d", len(cfg.Brokers))
	}
	if cfg.Brokers[0] != "broker1:9092" || cfg.Brokers[1] != "broker2:9092" {
		t.Fatalf("unexpected brokers: %v", cfg.Brokers)
	}
	if cfg.Topic != "custom-topic" {
		t.Fatalf("unexpected topic: %s", cfg.Topic)
	}
	if cfg.ConsumerGroup != "custom-group" {
		t.Fatalf("unexpected consumer group: %s", cfg.ConsumerGroup)
	}
}

func TestRelayWithoutKafka(t *testing.T) {
	relay := NewRelay()
	if relay.kafka != nil {
		t.Fatal("expected kafka field to be nil by default")
	}
	// Ensure broadcast works without Kafka.
	event := &Event{
		ID:        "test",
		Kind:      1,
		Tags:      [][]string{},
		CreatedAt: time.Now().Unix(),
	}
	relay.broadcast(event, nil)
}

func TestKafkaRoundTrip(t *testing.T) {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("KAFKA_BROKERS not set, skipping integration test")
	}

	topic := "nostr-test-" + time.Now().Format("20060102150405")

	relay := NewRelay()
	cfg := KafkaConfig{
		Brokers:       []string{brokers},
		Topic:         topic,
		ConsumerGroup: "test-group-" + time.Now().Format("150405"),
		Enabled:       true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	kb := NewKafkaBus(cfg, relay)
	relay.kafka = kb
	kb.Start(ctx)
	defer kb.Close()

	event := &Event{
		ID:        "abc123",
		PubKey:    "pubkey123",
		CreatedAt: time.Now().Unix(),
		Kind:      1,
		Tags:      [][]string{},
		Content:   "hello kafka",
	}

	kb.Publish(ctx, event)

	// Also verify via a direct reader that the message arrived.
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{brokers},
		Topic:     topic,
		Partition: 0,
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	defer reader.Close()

	readCtx, readCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readCancel()

	msg, err := reader.ReadMessage(readCtx)
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var received Event
	if err := json.Unmarshal(msg.Value, &received); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if received.ID != event.ID {
		t.Fatalf("expected event ID %s, got %s", event.ID, received.ID)
	}
	if received.Content != event.Content {
		t.Fatalf("expected content %q, got %q", event.Content, received.Content)
	}
}
