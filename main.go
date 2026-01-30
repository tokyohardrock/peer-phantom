package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	multiaddr "github.com/multiformats/go-multiaddr"
)

const (
	PROTOCOL = "/peer-phantom/1.0.0"
	KEY_FILE = "key"
)

func initPeer() (host.Host, error) {
	privKey, err := loadPrivateKey(KEY_FILE)
	if err != nil {
		return nil, err
	}

	host, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
		),
		libp2p.Identity(privKey),
	)

	return host, err
}

func loadPrivateKey(file string) (crypto.PrivKey, error) {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		privKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, err
		}

		keyAsBytes, err := crypto.MarshalPrivateKey(privKey)
		if err != nil {
			return nil, err
		}

		err = os.WriteFile(file, keyAsBytes, 0600)
		if err != nil {
			return nil, err
		}

		return privKey, nil
	}

	privKey, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	return crypto.UnmarshalPrivateKey(privKey)
}

func readMessage(s network.Stream){
	defer s.Close()

	reader := bufio.NewReader(s)
	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		fmt.Printf("Received: %s", message)
	}
}

func main(){
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	host, err := initPeer()
	if err != nil {
		log.Fatal("Failed to initialize peer: ", err)
	}
	defer host.Close()

	host.SetStreamHandler(PROTOCOL, readMessage)

	fmt.Println("=== Peer Phantom ===")
	fmt.Println("My Peer ID:", host.ID())
	fmt.Println("My addresses:")
	for _, addr := range host.Addrs() {
		fmt.Printf("  %s/p2p/%s\n", addr, host.ID())
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	var maddrStr string

	for {
		fmt.Print("Enter peer multiaddress: ")

		maddrStr, err = reader.ReadString('\n')
		if err == nil {
			break
		}

		log.Println("Failed to parse message: ", err)
	}
	maddrStr = strings.TrimSpace(maddrStr)

	maddr, err := multiaddr.NewMultiaddr(maddrStr)
	if err != nil {
		log.Fatal("Invalid multiaddress:", err)
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		log.Fatal("Invalid peer address:", err)
	}

	fmt.Println("Connecting to", info.ID, "...")
	err = host.Connect(ctx, *info)
	if err != nil {
		log.Fatal("Connection failed:", err)
	}

	stream, err := host.NewStream(ctx, info.ID, PROTOCOL)
	if err != nil {
		log.Fatal("Failed to create new stream to chosen peer: ", err)
	}
	defer stream.Close()

	fmt.Println("Connected! Type messages (Ctrl+C to quit):")
	go func() {
		for {
			fmt.Print("> ")
			message, err := reader.ReadString('\n')
			if err != nil {
				log.Println("Failed to parse message: ", err)
				break
			}

			_, err = stream.Write([]byte(message))
			if err != nil {
				log.Println("Failed to send message: ", err)
				break
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("Goodbye!")
}