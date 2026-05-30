package peer

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"errors"
	"fmt"
	log "log/slog"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"peer-phantom/internal/defs"
	"peer-phantom/internal/e2ee"
	"peer-phantom/internal/utils"
)

const (
	PROTOCOL = "/peer-phantom/1.0.0"
	KEY_FILE = "key" // the name of the file that will contain the identifier
	TIMEOUT  = 5
)

type SafeStream struct {
	Stream network.Stream
	Key    []byte
	isInit chan struct{}
}

type Peer struct {
	Host     host.Host
	MyPeerID string

	PrivKey        crypto.PrivKey
	SessionPrivKey *ecdh.PrivateKey
	SessionPubKeys []byte

	Streams    map[string]*SafeStream
	StreamsMut sync.RWMutex

	Chats  *defs.ChatStorage
	Broker defs.Broker
}

func (P *Peer) readBroker(ctx context.Context) {
	const fn = "peer.readBroker"

	for {
		chat, ok := <-P.Broker.UpdateOnBack
		if !ok {
			log.Error(
				fmt.Sprintf("%s: broker chan is closed", fn),
			)
			return
		}

		info, err := P.ConnectToPeer(ctx, chat.GetRemoteUser())
		if err != nil {
			log.Error(
				fmt.Sprintf("%s: during connecting to peer %w", fn, err),
			)
			continue
		}

		s, err := P.GetStreamToPeer(ctx, info.ID)
		if err != nil {
			log.Error(
				fmt.Sprintf("%s: during getting stream to peer %w", fn, err),
			)
			continue
		}

		pendingMsgs := make([]*defs.Message, 0, 3)

		chat.Mutex.RLock()

		for _, msg := range slices.Backward(chat.Messages) {
			if msg.Status == defs.Sent {
				break
			}

			if msg.Status == defs.Pending {
				pendingMsgs = append(pendingMsgs, msg)
			}
		}

		chat.Mutex.RUnlock()

		for _, msg := range slices.Backward(pendingMsgs) {
			msg.Mutex.Lock()

			err = P.WriteToStream(s.Stream, msg.Message)
			if err != nil {
				log.Error(
					fmt.Sprintf("%s: during writing to stream %w", fn, err),
				)

				msg.Status = defs.Error
				msg.Mutex.Unlock()
				continue
			}

			msg.Status = defs.Sent
			msg.Mutex.Unlock()
		}
	}
}

func (P *Peer) Init(ctx context.Context, chats *defs.ChatStorage, broker defs.Broker) error {
	const fn = "peer.Init"

	advertiseIP := os.Getenv("ADVERTISE_IP")
	if advertiseIP == "" {
		return fmt.Errorf("%s: ADVERTISE_IP is empty", fn)
	}

	privKey, err := loadPrivateKey(KEY_FILE)
	if err != nil {
		return fmt.Errorf("%s during loading private key: %w", fn, err)
	}

	host, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/4001"), // listen on all IPv4 addresses over TCP on port 4001
		libp2p.Identity(privKey),
		libp2p.AddrsFactory(func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
			res := make([]multiaddr.Multiaddr, 0, len(addrs))

			for _, addr := range addrs {
				port, err := addr.ValueForProtocol(multiaddr.P_TCP) // gets port from local multiaddress (4001)
				if err != nil {
					log.Error(
						fmt.Sprintf("%s something went wrong during port pasring: %w", fn, err),
					)
					continue
				}

				publicAddr, err := multiaddr.NewMultiaddr(
					fmt.Sprintf("/ip4/%s/tcp/%s", advertiseIP, port),
				)
				if err != nil {
					log.Error(
						fmt.Sprintf("%s something went wrong during new multiaddress validation: %w", fn, err),
					)
					continue
				}

				res = append(res, publicAddr)
			}

			return res
		}),
		libp2p.DisableIdentifyAddressDiscovery(),
	)
	if err != nil {
		return fmt.Errorf("%s during creating libp2p host: %w", fn, err)
	}

	P.Host = host
	P.PrivKey = privKey
	P.MyPeerID = host.ID().String()
	P.Streams = make(map[string]*SafeStream, 100)
	P.StreamsMut = sync.RWMutex{}
	P.Chats = chats
	P.Broker = broker

	P.SessionPrivKey, P.SessionPubKeys, err = e2ee.GenerateSessionKeys(privKey)
	if err != nil {
		return fmt.Errorf("%s during generating session keys: %w", fn, err)
	}

	host.SetStreamHandler(PROTOCOL, func(s network.Stream) {
		_, err := P.streamsHandler(s)
		if err != nil {
			fmt.Println("Error during handling incoming stream: ", err)
		}
	})

	go P.readBroker(ctx)

	return nil
}

func (P *Peer) ConnectToPeer(ctx context.Context, maddrStr string) (*peer.AddrInfo, error) {
	const fn = "peer.ConnectToPeer"

	ctx, cancel := context.WithTimeout(ctx, TIMEOUT*time.Second)
	defer cancel()

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

func (P *Peer) GetStreamToPeer(ctx context.Context, info peer.ID) (*SafeStream, error) {
	const fn = "peer.GetStreamToPeer"

	ctx, cancel := context.WithTimeout(ctx, TIMEOUT*time.Second)
	defer cancel()

	stream := P.CheckStream(info.String())
	if stream == nil {
		s, err := P.Host.NewStream(ctx, info, PROTOCOL)
		if err != nil {
			return nil, fmt.Errorf("%s during creating new stream: %w", fn, err)
		}

		stream, err = P.streamsHandler(s)
		if err != nil {
			return nil, fmt.Errorf("%s during handling stream: %w", fn, err)
		}
	}

	return stream, nil
}

func (P *Peer) getKeyToStream(remoteUser peer.ID, info []byte) ([]byte, error) {
	const fn = "peer.getKeyToStream"

	remotePubKey, err := remoteUser.ExtractPublicKey()
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
	stream := P.Streams[remoteUser.String()]
	copy(stream.Key, key)
	P.StreamsMut.Unlock()

	stream.isInit <- struct{}{}

	return key, nil
}

func (P *Peer) readFromStream(s network.Stream) {
	const fn = "peer.ReadFromStream"

	remoteUser := s.Conn().RemotePeer()
	reader := bufio.NewReader(s)

	var key []byte

	for {
		rawMessage, err := utils.ReadMessageWithLengthPrefix(reader)
		if err != nil {
			log.Error(
				fmt.Sprintf("%s during reading message with prefix: %v", fn, err),
			)
			return
		}

		if len(rawMessage) == 0 {
			log.Error(
				fmt.Sprintf("%s: empty message received", fn),
			)
			continue
		}

		if len(key) == 0 {
			key, err = P.getKeyToStream(remoteUser, rawMessage)
			if err != nil {
				log.Error(
					fmt.Sprintf("%s during getting key to stream: %v", fn, err),
				)
				return
			}
			continue
		}

		message, err := e2ee.DecryptMessage(key, rawMessage)
		if err != nil {
			log.Error(
				fmt.Sprintf("%s during decrypting message: %v", fn, err),
			)
			continue
		}

		err = P.UpdateChatHistory(remoteUser.String(), remoteUser.String(), string(message))
		if err != nil {
			log.Error(
				fmt.Sprintf("%s: during updating chat history %w", fn, err),
			)
			continue
		}
	}
}

func (P *Peer) WriteToStream(s network.Stream, rawMessage string) error {
	const fn = "peer.WriteToStream"

	remoteUser := s.Conn().RemotePeer().String()
	key := make([]byte, 32)

	stream := P.Streams[remoteUser]
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

	err = P.UpdateChatHistory(remoteUser, P.MyPeerID, rawMessage)
	if err != nil {
		return fmt.Errorf("%s during updating chat history: %w", fn, err)
	}

	return nil
}

func (P *Peer) UpdateChatHistory(remoteUser string, author string, message string) error {
	const fn = "peer.UpdateChatHistory"

	chat, err := P.Chats.GetChat(remoteUser)
	if err != nil && !errors.Is(err, defs.ErrorNoChat) {
		return fmt.Errorf("%s: %w", fn, err)
	}

	if err != nil {
		chat = P.Chats.AddChat(remoteUser)
	}

	chat.AppendMessage(author, message)
	if author != P.MyPeerID {
		chat.NewMessage()
	}

	P.Broker.UpdateOnFront <- chat

	return nil
}

func (P *Peer) streamsHandler(s network.Stream) (*SafeStream, error) {
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

	go P.readFromStream(s)

	return stream, nil
}
