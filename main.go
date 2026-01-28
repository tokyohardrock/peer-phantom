package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	libp2p "github.com/libp2p/go-libp2p"
)

func main(){
	host, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer host.Close()

	fmt.Println("Host ID: ", host.ID())
	fmt.Println("Addresses:")
	for _, addr := range host.Addrs() {
		fmt.Printf("  %s/p2p/%s\n", addr, host.ID())
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("Goodbye!")
}