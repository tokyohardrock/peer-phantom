package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"peer-phantom/internal/input"
	"peer-phantom/internal/logger"
	"peer-phantom/internal/peer"
)

func main() {
	ctx := context.TODO()

	sigChan := make(chan os.Signal, 1)
	go gracefulShutdown(sigChan)

	log := logger.InitLogger()

	var host peer.Peer

	err := host.Init(log)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	input.ListenStdin(ctx, log, &host)
}

func gracefulShutdown(sigChan chan os.Signal) {
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	os.Exit(0)
}
