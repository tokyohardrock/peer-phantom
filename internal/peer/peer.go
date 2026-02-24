package peer

import (
	"context"
	"crypto/ecdh"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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

func (P *Peer) Init() error {
	privKey, err := loadPrivateKey(KEY_FILE)
	if err != nil {
		return err
	}

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

	P.SessionPrivKey, P.SessionPubKeys, err = e2ee.GenerateSessionKeys(privKey)
	if err != nil {
		return err
	}

	host.SetStreamHandler(PROTOCOL, func(s network.Stream) {
		_, err := P.streamsHandler(s)
		if err != nil {
			fmt.Println("Errot during handling incoming stream: ", err)
		}
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

	err = P.Host.Connect(ctx, *info)
	if err != nil {
		return nil, err
	}

	return info, nil
}

func (P *Peer) CheckStream(info string) *SafeStream {
	P.StreamsMut.RLock()
	stream := P.Streams[info]
	P.StreamsMut.RUnlock()

	return stream
}

func (P *Peer) GetStreamToPeer(ctx context.Context, info peer.ID) (*SafeStream, error) {
	stream := P.CheckStream(info.String())
	if stream == nil {
		s, err := P.Host.NewStream(ctx, info, PROTOCOL)
		if err != nil {
			return nil, errors.New("GetStreamToPeer during creating new stream: " + err.Error())
		}

		stream, err = P.streamsHandler(s)
		if err != nil {
			return nil, err
		}
	}

	return stream, nil
}

func (P *Peer) getKeyToStream(remotePeerID peer.ID, info []byte) ([]byte, error) {
	remotePubKey, err := remotePeerID.ExtractPublicKey()
	if err != nil {
		return nil, err
	}

	remoteSessionPub, err := e2ee.VerifySessionPubKey(remotePubKey, info)
	if err != nil {
		return nil, err
	}

	secret, err := e2ee.ComputeSharedSecret(P.SessionPrivKey, remoteSessionPub)
	if err != nil {
		return nil, err
	}

	key, err := e2ee.DeriveAESKey(secret)
	if err != nil {
		return nil, err
	}

	return key, nil
}

func (P *Peer) ReadFromStream(s network.Stream) {
	remotePeerID := s.Conn().RemotePeer()

	var key []byte
	var size uint32

	for {
		err := binary.Read(s, binary.BigEndian, &size)
		if err != nil {
			fmt.Println(err)
			return
		}

		rawMessage := make([]byte, size)
		_, err = io.ReadFull(s, rawMessage)
		if err != nil {
			fmt.Println(err)
			return
		}

		if len(key) == 0 {
			key, err = P.getKeyToStream(remotePeerID, rawMessage)
			if err != nil {
				fmt.Println(err)
				return
			}

			P.StreamsMut.Lock()
			stream := P.Streams[remotePeerID.String()]
			copy(stream.Key, key)
			P.StreamsMut.Unlock()

			stream.isInit <- struct{}{}

			continue
		}

		message, err := e2ee.DecryptMessage(key, rawMessage)
		if err != nil {
			fmt.Println(err)
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
		return err
	}

	binary.Write(s, binary.BigEndian, uint32(len(message)))

	_, err = s.Write(message)
	if err != nil {
		return err
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

func (P *Peer) streamsHandler(s network.Stream) (*SafeStream, error) {
	stream := &SafeStream{
		Stream: s,
		Key:    make([]byte, 32),
		isInit: make(chan struct{}),
	}

	P.StreamsMut.Lock()
	P.Streams[s.Conn().RemotePeer().String()] = stream
	P.StreamsMut.Unlock()

	_, err := s.Write(P.SessionPubKeys)
	if err != nil {
		return nil, err
	}

	go P.ReadFromStream(s)

	return stream, nil
}
