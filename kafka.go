package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

// KafkaConfig holds the configuration for connecting to Kafka.
type KafkaConfig struct {
	Brokers       []string
	Topic         string
	ConsumerGroup string
	Enabled       bool
}

// KafkaConfigFromEnv reads Kafka configuration from environment variables.
// Kafka is enabled only when KAFKA_BROKERS is set.
func KafkaConfigFromEnv() KafkaConfig {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		return KafkaConfig{Enabled: false}
	}

	topic := os.Getenv("KAFKA_TOPIC")
	if topic == "" {
		topic = "nostr-events"
	}

	group := os.Getenv("KAFKA_CONSUMER_GROUP")
	if group == "" {
		hostname, _ := os.Hostname()
		group = "nostr-relay-" + hostname
	}

	return KafkaConfig{
		Brokers:       strings.Split(brokers, ","),
		Topic:         topic,
		ConsumerGroup: group,
		Enabled:       true,
	}
}

// KafkaBus handles publishing and consuming events via Kafka.
type KafkaBus struct {
	writer *kafka.Writer
	reader *kafka.Reader
	relay  *Relay
	done   chan struct{}
}

// NewKafkaBus creates a new KafkaBus with a writer and reader.
func NewKafkaBus(cfg KafkaConfig, relay *Relay) *KafkaBus {
	writer := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.Topic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireAll,
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: cfg.Brokers,
		Topic:   cfg.Topic,
		GroupID: cfg.ConsumerGroup,
	})

	return &KafkaBus{
		writer: writer,
		reader: reader,
		relay:  relay,
		done:   make(chan struct{}),
	}
}

// Publish sends an event to Kafka.
func (kb *KafkaBus) Publish(ctx context.Context, event *Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("kafka: failed to marshal event: %v", err)
		return
	}

	err = kb.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(event.ID),
		Value: data,
	})
	if err != nil {
		log.Printf("kafka: failed to publish event %s: %v", event.ID, err)
	}
}

// Start launches the consume loop in a goroutine.
func (kb *KafkaBus) Start(ctx context.Context) {
	go kb.consumeLoop(ctx)
}

func (kb *KafkaBus) consumeLoop(ctx context.Context) {
	defer close(kb.done)
	for {
		msg, err := kb.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("kafka: read error: %v", err)
			continue
		}

		var event Event
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("kafka: failed to unmarshal event: %v", err)
			continue
		}

		if event.Tags == nil {
			event.Tags = [][]string{}
		}

		// Ephemeral events skip the store, broadcast directly.
		if IsEphemeral(event.Kind) {
			kb.relay.broadcast(&event, nil)
			continue
		}

		accepted, msg2 := kb.relay.store.StoreEvent(&event)
		if accepted && msg2 != "duplicate: already have this event" {
			kb.relay.broadcast(&event, nil)
		}
	}
}

// Close shuts down the writer and reader.
func (kb *KafkaBus) Close() {
	if err := kb.writer.Close(); err != nil {
		log.Printf("kafka: writer close error: %v", err)
	}
	if err := kb.reader.Close(); err != nil {
		log.Printf("kafka: reader close error: %v", err)
	}
}
