package peer

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/libp2p/go-libp2p/core/crypto"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"peer-phantom/internal/e2ee"
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

type SafeStream struct {
	Stream network.Stream
	Secret []byte
}

type Peer struct {
	Host    host.Host
	PrivKey crypto.PrivKey

	SessionPrivKey      *ecdh.PrivateKey
	SessionPubKey       *ecdh.PublicKey
	SignedSessionPubKey []byte

	MyPeerID     string
	ActivePeerID atomic.Value

	Streams    map[string]*SafeStream
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
	P.PrivKey = privKey
	P.MyPeerID = host.ID().String()
	P.ActivePeerID.Store("")
	P.Streams = make(map[string]*SafeStream, 100)
	P.StreamsMut = sync.RWMutex{}
	P.Chats = make(map[string][]Mssg, 100)
	P.ChatsMut = sync.RWMutex{}
	P.NewMessage = make(chan struct{}, 1)

	P.SessionPrivKey, P.SessionPubKey, P.SignedSessionPubKey, err = e2ee.GenerateSessionKeys(privKey)
	if err != nil {
		return err
	}

	host.SetStreamHandler(PROTOCOL, func(s network.Stream) {
		_ = P.streamsHandler(s)
	})

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

func (P *Peer) CheckStream(ctx context.Context, info string) *SafeStream {
	P.StreamsMut.RLock()
	stream := P.Streams[info]
	P.StreamsMut.RUnlock()

	return stream
}

func (P *Peer) GetStreamToPeer(ctx context.Context, info peer.ID) (*SafeStream, error) {
	stream := P.CheckStream(ctx, info.String())
	if stream == nil {
		s, err := P.Host.NewStream(ctx, info, PROTOCOL)
		if err != nil {
			return nil, errors.New("GetStreamToPeer: " + err.Error())
		}

		stream = P.streamsHandler(s)
	}

	return stream, nil
}

func (P *Peer) ReadFromStream(s network.Stream) {
	remotePeerID := s.Conn().RemotePeer().String()
	reader := bufio.NewReader(s)

	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			log.Println(err)
			return
		}

		P.UpdateChatHistory(remotePeerID,
			Mssg{
				Author:  utils.GetShortPeerID(remotePeerID),
				Message: message,
			},
		)
	}
}

func (P *Peer) WriteToStream(s network.Stream, message string) error {
	remotePeerID := s.Conn().RemotePeer().String()

	_, err := s.Write([]byte(message))
	if err != nil {
		return err
	}

	P.UpdateChatHistory(remotePeerID,
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

func (P *Peer) streamsHandler(s network.Stream) *SafeStream {
	stream := &SafeStream{
		Stream: s,
		Secret: make([]byte, 0, 32),
	}

	P.StreamsMut.Lock()
	P.Streams[s.Conn().RemotePeer().String()] = stream
	P.StreamsMut.Unlock()

	go P.ReadFromStream(s)

	return stream
}
