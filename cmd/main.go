package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"peer-phantom/internal/input"
	"peer-phantom/internal/peer"
)

func main() {
	ctx := context.TODO()

	sigChan := make(chan os.Signal, 1)
	go gracefulShutdown(sigChan)

	var host peer.Peer

	err := host.Init()
	if err != nil {
		log.Fatal("Unable to initialize peer: ", err)
	}

	input.ListenStdin(ctx, &host)
}

func gracefulShutdown(sigChan chan os.Signal) {
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	os.Exit(0)
}
