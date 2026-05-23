package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"technical-specification-review-agent/internal/app"
)

func main() {
	application, err := app.New()
	if err != nil {
		log.Fatalf("bootstrap app: %v", err)
	}

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		<-signals

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := application.Shutdown(ctx); err != nil {
			log.Printf("shutdown app: %v", err)
		}
	}()

	if err := application.Run(); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
