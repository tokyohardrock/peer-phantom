package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"syscall"
	"os/signal"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	multiaddr "github.com/multiformats/go-multiaddr"

	"p2pchat/terminal"
)

const (
	PROTOCOL = "/peer-phantom/1.0.0"
	KEY_FILE = "key"
)

type mssg struct {
	author string
	message string
}

var streams = make(map[string]network.Stream, 30)
var streamsMutex = sync.Mutex{}

var chats = make(map[string][]mssg, 30)
var chatsMutex = sync.Mutex{}

var myPeerID, remotePeerID string

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

func printPeerInfo(host host.Host) {
	fmt.Println("Peer ID:", host.ID())
	fmt.Println("Addresses:")
	for _, addr := range host.Addrs() {
		fmt.Printf("  %s/p2p/%s\n", addr, host.ID())
	}
	fmt.Println()
}

func trimInput(reader *bufio.Reader) (string, error) {
	maddrStr, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return  strings.TrimSpace(maddrStr), nil
}

func connectToPeer(ctx context.Context, maddrStr string, host host.Host) (*peer.AddrInfo, error) {
	maddr, err := multiaddr.NewMultiaddr(maddrStr)
	if err != nil {
		return nil, err
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return nil, err
	}

	fmt.Println("Connecting to", info.ID, "...")
	err = host.Connect(ctx, *info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func readMessage(s network.Stream){
	senderPeerID := s.Conn().RemotePeer().String()
	reader := bufio.NewReader(s)

	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			log.Println("Failed to read message: ", err)
			return
		}

		updateHistory(senderPeerID, senderPeerID, message)
	}
}

func writeMessage(reader *bufio.Reader, s network.Stream) {
	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			log.Println("Failed to parse message: ", err)
			return
		}

		_, err = s.Write([]byte(message))
		if err != nil {
			log.Println("Failed to send message: ", err)
			return
		}

		updateHistory(myPeerID, s.Conn().RemotePeer().String(), message)
	}
}

func shortPeerID(peerID string) string {
	if len(peerID) > 6 {
		return peerID[len(peerID)-6:]
	}
	return peerID
}

func updateHistory(author string, peerID string, message string) {
	chatsMutex.Lock()

	if chats[peerID] == nil {
		chats[peerID] = make([]mssg, 0, 100)
	}

	chats[peerID] = append(chats[peerID], mssg{
		author: shortPeerID(author),
		message: message,
	})

	chatsMutex.Unlock()

	fmt.Print(shortPeerID(author), ": ", message)
}

func streamHandler(s network.Stream) {
	streamsMutex.Lock()
	streams[s.Conn().RemotePeer().String()] = s
	streamsMutex.Unlock()
}

func gracefulShutdown(sigChan chan os.Signal) {
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Printf("\n%d\n", len(streams))
	os.Exit(0)
}

func main(){
	terminal.Clear()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	go gracefulShutdown(sigChan)

	host, err := initPeer()
	if err != nil {
		log.Fatal("Failed to initialize peer: ", err)
	}
	defer host.Close()

	myPeerID = host.ID().String()

	host.SetStreamHandler(PROTOCOL, streamHandler)

	fmt.Println("-=* Peer Phantom *=-")
	printPeerInfo(host)

	reader := bufio.NewReader(os.Stdin)
	for {
		remotePeerID = ""

		fmt.Print("Enter multiaddress: ")

		maddr, err := trimInput(reader)
		if err != nil {
			log.Println(err)
			continue
		}

		info, err := connectToPeer(ctx, maddr, host)
		if err != nil {
			log.Println("! Failed to connect to peer: ", err)
			continue
		}

		remotePeerID = info.ID.String()

		streamsMutex.Lock()

		stream := streams[remotePeerID]

		if stream == nil {
			stream, err = host.NewStream(ctx, info.ID, PROTOCOL)
			if err != nil {
				log.Println("Failed to create new stream to chosen peer: ", err)
				continue
			}

			streams[remotePeerID] = stream
		}

		streamsMutex.Unlock()

		go readMessage(stream)
		writeMessage(reader, stream)
	}
}