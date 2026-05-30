package main

import (
	"context"
	"fmt"
	log "log/slog"
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

	f, err := os.OpenFile("debug.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer f.Close()

	log.SetDefault(logger.InitLogger(f))

	chats := defs.InitChatStorage()
	broker := defs.InitBroker()

	var host peer.Peer

	err = host.Init(ctx, chats, broker)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	m := make([]string, 0, 3)
	gg := host.Host.Addrs()

	for i := range gg {
		m = append(m, fmt.Sprintf("%s/p2p/%s", gg[i].String(), host.MyPeerID))
	}

	err = tui.Run(chats, broker, m)
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
