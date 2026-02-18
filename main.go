package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	relay := NewRelay()

	kafkaCfg := KafkaConfigFromEnv()
	if kafkaCfg.Enabled {
		log.Printf("Kafka enabled: brokers=%v topic=%s group=%s", kafkaCfg.Brokers, kafkaCfg.Topic, kafkaCfg.ConsumerGroup)
		ctx, cancel := context.WithCancel(context.Background())
		kb := NewKafkaBus(kafkaCfg, relay)
		relay.kafka = kb
		kb.Start(ctx)
		defer func() {
			cancel()
			kb.Close()
		}()
	} else {
		log.Println("Kafka disabled (KAFKA_BROKERS not set)")
	}

	http.Handle("/", relay)

	server := &http.Server{Addr: ":9090"}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		server.Close()
	}()

	log.Println("Nostr relay listening on :9090")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
