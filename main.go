package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	multiaddr "github.com/multiformats/go-multiaddr"

	"peer-phantom/terminal"
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
var chatsMutex = sync.RWMutex{}

var myPeerID, remotePeerID string

var newMessages = make(chan struct{}, 1)

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

func streamsHandler(s network.Stream) {
	streamsMutex.Lock()
	streams[s.Conn().RemotePeer().String()] = s
	streamsMutex.Unlock()

	go readMessage(s)
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

func getStreamToPeer(ctx context.Context, host host.Host, info *peer.AddrInfo) error {
	var err error

	streamsMutex.Lock()

	stream := streams[remotePeerID]
	if stream == nil {
		stream, err = host.NewStream(ctx, info.ID, PROTOCOL)
		if err != nil {
			return err
		}

		streams[remotePeerID] = stream
		go readMessage(stream)
	}

	streamsMutex.Unlock()

	return nil
}

func concatenateStrings(seq ...string) string {
	var length, byteIdx int
	for i := range seq {
		length += len(seq[i])
	}

	if length == 0 {
		return ""
	}

	b := make([]byte, length)
	for i := range seq {
		byteIdx += copy(b[byteIdx:], seq[i])
	}

	return unsafe.String(&b[0] , length)
}

func getShortPeerID(peerID string) string {
	if len(peerID) > 6 {
		return peerID[len(peerID)-6:]
	}

	return peerID
}

func readMessage(s network.Stream){
	senderPeerID := s.Conn().RemotePeer().String()
	reader := bufio.NewReader(s)

	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			log.Println(err)
			return
		}

		updateHistory(senderPeerID, getShortPeerID(senderPeerID), message)
	}
}

func writeMessage(s network.Stream, message string) error {
	_, err := s.Write([]byte(message))
	if err != nil {
		return err
	}

	updateHistory(s.Conn().RemotePeer().String(), getShortPeerID(myPeerID), message)

	return nil
}

func updateHistory(peerID string, author string, message string) {
	chatsMutex.Lock()

	if chats[peerID] == nil {
		chats[peerID] = make([]mssg, 0, 100)
	}

	chats[peerID] = append(chats[peerID], mssg{
		author: author,
		message: message,
	})

	chatsMutex.Unlock()

	if peerID != remotePeerID {
		return
	}

	select{
	case newMessages<-struct{}{}:
	default:
	}
}

func showChat() {
	terminal.Clear()

	chatsMutex.RLock()

	snapshot := append([]mssg(nil), chats[remotePeerID]...)
	lastPrinted := len(snapshot)

	chatsMutex.RUnlock()

	for i := range snapshot {
		fmt.Print(snapshot[i].message)
	}

	for {
		<-newMessages

		if remotePeerID == "" {
			return
		}

		chatsMutex.RLock()

		unreadedMessages := append([]mssg(nil), chats[remotePeerID][lastPrinted:]...)
		lastPrinted = len(chats[remotePeerID])

		chatsMutex.RUnlock()

		for i := range unreadedMessages {
			fmt.Print(unreadedMessages[i].message)
		}
	}
}

func commandHandler(ctx context.Context, host host.Host, command []string) error {
	switch command[0] {
	case "/conn":
		if len(command) == 1 {
			return errors.New("Not enough arguments!")
		}
		maddr := command[1]

		info, err := connectToPeer(ctx, maddr, host)
		if err != nil {
			return err
		}

		remotePeerID = info.ID.String()

		err = getStreamToPeer(ctx, host, info)
		if err != nil {
			return err
		}

		go showChat()
		writeMessage(streams[remotePeerID], concatenateStrings(getShortPeerID(myPeerID), " joined the chat\n"))
	case "/back":
		if remotePeerID != "" {
			writeMessage(streams[remotePeerID], concatenateStrings(getShortPeerID(myPeerID), " leaved the chat\n"))
			return errors.New("Leaving chat ...")
		} else {
			fmt.Println("Press Ctrl+C to exit")
		}
	default:
		fmt.Println("Unknown command!")
	}

	return nil
}

func inputHandler(ctx context.Context, host host.Host, rawInput string) error {
	words := strings.Fields(rawInput)

	if len(words) > 0 && rawInput[0] == '/' {
		err := commandHandler(ctx, host, words)
		if err != nil {
			return err
		}

		return nil
	}

	if _, ok := streams[remotePeerID]; !ok {
		return errors.New("Unable to find a stream for the specified peer")
	}

	err := writeMessage(streams[remotePeerID], concatenateStrings(getShortPeerID(myPeerID), ": ", rawInput))

	return err
}

func main(){
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	go gracefulShutdown(sigChan)

	reader := bufio.NewReader(os.Stdin)

	host, err := initPeer()
	if err != nil {
		log.Fatal("Failed to initialize peer: ", err)
	}
	defer host.Close()

	myPeerID = host.ID().String()

	host.SetStreamHandler(PROTOCOL, streamsHandler)

	for {
		remotePeerID = ""

		terminal.Clear()

		fmt.Println("-=] Peer Phantom [=-")
		fmt.Println("Your addresses:")
		for i, addr := range host.Addrs() {
			fmt.Printf("%d. %s/p2p/%s\n", i+1, addr, myPeerID)
		}
		fmt.Println()

		fmt.Println("Avaliable commands: ")
		fmt.Println("\t/conn multiaddr")
		fmt.Println("\t/back")

		for {
			input, err := reader.ReadString('\n')
			if err != nil {
				log.Println(err)
				continue
			}

			err = inputHandler(ctx, host, input)
			if err != nil {
				break
			}
		}
	}
}

func gracefulShutdown(sigChan chan os.Signal) {
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Printf("\n%d\n", len(streams))
	os.Exit(0)
}