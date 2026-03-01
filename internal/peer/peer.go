package peer

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"fmt"
	"log/slog"
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
	Key    []byte
	isInit chan struct{}
}

type Peer struct {
	Host    host.Host
	PrivKey crypto.PrivKey

	SessionPrivKey *ecdh.PrivateKey
	SessionPubKeys []byte

	MyPeerID     string
	ActivePeerID atomic.Value

	Streams    map[string]*SafeStream
	StreamsMut sync.RWMutex

	Chats    map[string][]Mssg
	ChatsMut sync.RWMutex

	NewMessage chan struct{}
}

func (P *Peer) Init(log *slog.Logger) error {
	const fn = "peer.Init"

	privKey, err := loadPrivateKey(KEY_FILE)
	if err != nil {
		return fmt.Errorf("%s during loading private key: %w", fn, err)
	}

	host, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
		), // listen on all IPv4 addresses over TCP on a random port
		libp2p.Identity(privKey),
	)
	if err != nil {
		return fmt.Errorf("%s during creating libp2p host: %w", fn, err)
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

	P.SessionPrivKey, P.SessionPubKeys, err = e2ee.GenerateSessionKeys(privKey)
	if err != nil {
		return fmt.Errorf("%s during generating session keys: %w", fn, err)
	}

	host.SetStreamHandler(PROTOCOL, func(s network.Stream) {
		_, err := P.streamsHandler(log, s)
		if err != nil {
			fmt.Println("Errot during handling incoming stream: ", err)
		}
	})

	return nil
}

func (P *Peer) ConnectToPeer(ctx context.Context, maddrStr string) (*peer.AddrInfo, error) {
	const fn = "peer.ConnectToPeer"

	maddr, err := multiaddr.NewMultiaddr(maddrStr)
	if err != nil {
		return nil, fmt.Errorf("%s during parsing multiaddress: %w", fn, err)
	}

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return nil, fmt.Errorf("%s during extracting peer info from multiaddress: %w", fn, err)
	}

	err = P.Host.Connect(ctx, *info)
	if err != nil {
		return nil, fmt.Errorf("%s during connecting to peer: %w", fn, err)
	}

	return info, nil
}

func (P *Peer) CheckStream(info string) *SafeStream {
	P.StreamsMut.RLock()
	stream := P.Streams[info]
	P.StreamsMut.RUnlock()

	return stream
}

func (P *Peer) GetStreamToPeer(ctx context.Context, log *slog.Logger, info peer.ID) (*SafeStream, error) {
	const fn = "peer.GetStreamToPeer"

	stream := P.CheckStream(info.String())
	if stream == nil {
		s, err := P.Host.NewStream(ctx, info, PROTOCOL)
		if err != nil {
			return nil, fmt.Errorf("%s during creating new stream: %w", fn, err)
		}

		stream, err = P.streamsHandler(log, s)
		if err != nil {
			return nil, fmt.Errorf("%s during handling stream: %w", fn, err)
		}
	}

	return stream, nil
}

func (P *Peer) getKeyToStream(remotePeerID peer.ID, info []byte) ([]byte, error) {
	const fn = "peer.getKeyToStream"

	remotePubKey, err := remotePeerID.ExtractPublicKey()
	if err != nil {
		return nil, fmt.Errorf("%s during extracting public key from peer ID: %w", fn, err)
	}

	remoteSessionPub, err := e2ee.VerifySessionPubKey(remotePubKey, info)
	if err != nil {
		return nil, fmt.Errorf("%s during verifying session public key: %w", fn, err)
	}

	secret, err := e2ee.ComputeSharedSecret(P.SessionPrivKey, remoteSessionPub)
	if err != nil {
		return nil, fmt.Errorf("%s during computing shared secret: %w", fn, err)
	}

	key, err := e2ee.DeriveAESKey(secret)
	if err != nil {
		return nil, fmt.Errorf("%s during deriving AES key: %w", fn, err)
	}

	P.StreamsMut.Lock()
	stream := P.Streams[remotePeerID.String()]
	copy(stream.Key, key)
	P.StreamsMut.Unlock()

	stream.isInit <- struct{}{}

	return key, nil
}

func (P *Peer) ReadFromStream(log *slog.Logger, s network.Stream) {
	const fn = "peer.ReadFromStream"

	remotePeerID := s.Conn().RemotePeer()
	reader := bufio.NewReader(s)

	var key []byte

	for {
		rawMessage, err := utils.ReadMessageWithLengthPrefix(reader)
		if err != nil {
			log.Error(fmt.Sprintf("%s during reading message with prefix: %v", fn, err))
			return
		}

		if len(rawMessage) < 2 {
			log.Error(fmt.Sprintf("%s: empty message received", fn))
			continue
		}

		if len(key) == 0 {
			key, err = P.getKeyToStream(remotePeerID, rawMessage)
			if err != nil {
				log.Error(fmt.Sprintf("%s during getting key to stream: %v", fn, err))
				return
			}

			continue
		}

		message, err := e2ee.DecryptMessage(key, rawMessage)
		if err != nil {
			log.Error(fmt.Sprintf("%s during decrypting message: %v", fn, err))
			return
		}

		P.UpdateChatHistory(remotePeerID.String(),
			Mssg{
				Author:  utils.GetShortPeerID(remotePeerID.String()),
				Message: string(message),
			},
		)
	}
}

func (P *Peer) WriteToStream(s network.Stream, rawMessage string) error {
	const fn = "peer.WriteToStream"

	remotePeerID := s.Conn().RemotePeer().String()
	key := make([]byte, 32)

	stream := P.Streams[remotePeerID]
	_, ok := <-stream.isInit
	if ok {
		close(stream.isInit)
	}

	P.StreamsMut.RLock()
	copy(key, stream.Key)
	P.StreamsMut.RUnlock()

	message, err := e2ee.EncryptMessage(key, []byte(rawMessage))
	if err != nil {
		return fmt.Errorf("%s during encrypting message: %w", fn, err)
	}

	_, err = s.Write(utils.AddLengthPrefixToMessage(message))
	if err != nil {
		return fmt.Errorf("%s during writing message to stream: %w", fn, err)
	}

	P.UpdateChatHistory(remotePeerID,
		Mssg{
			Author:  P.MyPeerID,
			Message: rawMessage,
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

func (P *Peer) streamsHandler(log *slog.Logger, s network.Stream) (*SafeStream, error) {
	const fn = "peer.streamsHandler"

	stream := &SafeStream{
		Stream: s,
		Key:    make([]byte, 32),
		isInit: make(chan struct{}),
	}

	P.StreamsMut.Lock()
	P.Streams[s.Conn().RemotePeer().String()] = stream
	P.StreamsMut.Unlock()

	_, err := s.Write(utils.AddLengthPrefixToMessage(P.SessionPubKeys))
	if err != nil {
		return nil, fmt.Errorf("%s during writing session public keys to stream: %w", fn, err)
	}

	go P.ReadFromStream(log, s)

	return stream, nil
}
