package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"peer-phantom/internal/defs"
	"peer-phantom/internal/logger"
	"peer-phantom/internal/peer"
	"peer-phantom/internal/tui"
)

func main() {
	ctx := context.Background()

	sigChan := make(chan os.Signal, 1)
	go gracefulShutdown(sigChan)

	log := logger.InitLogger()

	chats := defs.InitChatStorage()
	broker := defs.InitBroker()

	var host peer.Peer

	err := host.Init(ctx, log, chats, broker)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	err = tui.Run(chats, broker)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

func gracefulShutdown(sigChan chan os.Signal) {
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	os.Exit(0)
}
