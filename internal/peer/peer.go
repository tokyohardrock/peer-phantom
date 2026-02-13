package peer

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"peer-phantom/internal/utils"
)

const (
	PROTOCOL = "/peer-phantom/1.0.0"
	KEY_FILE = "key" // the name of the file that will contain the identifier
)

type Mssg struct {
	Author  string
	Message string
}

type Peer struct {
	Host host.Host

	MyPeerID     string
	ActivePeerID atomic.Value

	Streams    map[string]network.Stream
	StreamsMut sync.RWMutex

	Chats    map[string][]Mssg
	ChatsMut sync.RWMutex

	NewMessage chan struct{}
}

func (P *Peer) Init() error {
	privKey, err := loadPrivateKey(KEY_FILE)
	if err != nil {
		return err
	}

	// raise peer
	host, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
		), // listen on all IPv4 addresses over TCP on a random port
		libp2p.Identity(privKey),
	)
	if err != nil {
		return err
	}

	P.Host = host
	P.MyPeerID = host.ID().String()
	P.ActivePeerID.Store("")
	P.Streams = make(map[string]network.Stream, 100)
	P.StreamsMut = sync.RWMutex{}
	P.Chats = make(map[string][]Mssg, 100)
	P.ChatsMut = sync.RWMutex{}
	P.NewMessage = make(chan struct{}, 1)

	host.SetStreamHandler(PROTOCOL, P.streamsHandler)

	return nil
}

func (P *Peer) ConnectToPeer(ctx context.Context, maddrStr string) (*peer.AddrInfo, error) {
	maddr, err := multiaddr.NewMultiaddr(maddrStr)
	if err != nil {
		return nil, err
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return nil, err
	}

	fmt.Println("Connecting to", info.ID, "...")
	err = P.Host.Connect(ctx, *info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (P *Peer) GetStreamToPeer(ctx context.Context, info *peer.AddrInfo) error {
	P.StreamsMut.RLock()
	stream := P.Streams[info.ID.String()]
	P.StreamsMut.RUnlock()

	if stream == nil {
		stream, err := P.Host.NewStream(ctx, info.ID, PROTOCOL)
		if err != nil {
			return err
		}

		P.streamsHandler(stream)
	}

	return nil
}

func (P *Peer) ReadFromStream(s network.Stream) {
	senderPeerID := s.Conn().RemotePeer().String()
	reader := bufio.NewReader(s)

	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			log.Println(err)
			return
		}

		P.UpdateChatHistory(senderPeerID,
			Mssg{
				Author:  utils.GetShortPeerID(senderPeerID),
				Message: message,
			},
		)
	}
}

func (P *Peer) WriteToStream(message string) error {
	activePeerID := P.ActivePeerID.Load().(string)

	P.StreamsMut.RLock()
	stream := P.Streams[activePeerID]
	P.StreamsMut.RUnlock()

	_, err := stream.Write([]byte(message))
	if err != nil {
		return err
	}

	P.UpdateChatHistory(activePeerID,
		Mssg{
			Author:  P.MyPeerID,
			Message: message,
		},
	)

	return nil
}

func (P *Peer) UpdateChatHistory(remotePeerID string, message Mssg) {
	P.ChatsMut.Lock()

	if P.Chats[remotePeerID] == nil {
		P.Chats[remotePeerID] = make([]Mssg, 0, 100)
	}

	P.Chats[remotePeerID] = append(P.Chats[remotePeerID], message)

	P.ChatsMut.Unlock()

	if remotePeerID != P.ActivePeerID.Load().(string) {
		return
	}

	select {
	case P.NewMessage <- struct{}{}:
	default:
	}
}

func (P *Peer) streamsHandler(s network.Stream) {
	P.StreamsMut.Lock()
	P.Streams[s.Conn().RemotePeer().String()] = s
	P.StreamsMut.Unlock()

	go P.ReadFromStream(s)
}
