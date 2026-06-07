package peer

import (
	"context"
	"crypto/ecdh"
	"errors"
	"fmt"
	log "log/slog"
	"os"
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

func (P *Peer) closeFailedConnection(chat *defs.ChatData) {
	P.DeleteStreamToPeer(chat.GetChatID())
	chat.SetConnStatus(defs.Failed)

	P.Broker.UpdateOnFront <- chat
}

func (P *Peer) readBroker(ctx context.Context) {
	const fn = "peer.readBroker"

	for {
		select {
		case <-ctx.Done():
			return
		case chat, ok := <-P.Broker.UpdateOnBack:
			if !ok {
				log.Error(
					fmt.Sprintf("%s: broker chan is closed", fn),
				)
				return
			}

			info, err := P.ConnectToPeer(ctx, chat.GetRemoteAddress())
			if err != nil {
				P.closeFailedConnection(chat)

				log.Error(
					fmt.Sprintf("%s: during connecting to peer %w", fn, err),
				)
				continue
			}

			s, err := P.GetStreamToPeer(ctx, info.ID)
			if err != nil {
				P.closeFailedConnection(chat)

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
}

func (P *Peer) refreshConnections(ctx context.Context) {
	const fn = "peer.refreshConnections"

	refreshRate := 250 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(refreshRate):
			chats := P.Chats.GetChatSlice()

			for _, chat := range chats {
				if chat.GetConnStatus() == defs.Failed {
					chat.SetConnStatus(defs.Connecting)
					P.Broker.UpdateOnBack <- chat
				}
			}
		}
	}
}

func (P *Peer) Init(ctx context.Context, chats *defs.ChatStorage, broker defs.Broker) error {
	const fn = "peer.Init"

	// advertiseIP := os.Getenv("ADVERTISE_IP")
	// if advertiseIP == "" {
	// 	return fmt.Errorf("%s: ADVERTISE_IP is empty", fn)
	// }

	port := os.Getenv("PORT")
	if port == "" {
		return fmt.Errorf("%s: port is not specified", fn)
	}

	privKey, err := loadPrivateKey()
	if err != nil {
		return fmt.Errorf("%s during loading private key: %w", fn, err)
	}

	host, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%s", port)), // listen on all IPv4 addresses over TCP on a specified port
		libp2p.Identity(privKey),
		libp2p.NoSecurity, // disables default libp2p transport encryption
		// libp2p.AddrsFactory(func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
		// 	res := make([]multiaddr.Multiaddr, 0, len(addrs))

		// 	for _, addr := range addrs {
		// 		port, err := addr.ValueForProtocol(multiaddr.P_TCP) // gets port from local multiaddress (4001)
		// 		if err != nil {
		// 			log.Error(
		// 				fmt.Sprintf("%s something went wrong during port pasring: %w", fn, err),
		// 			)
		// 			continue
		// 		}

		// 		publicAddr, err := multiaddr.NewMultiaddr(
		// 			fmt.Sprintf("/ip4/%s/tcp/%s", advertiseIP, port),
		// 		)
		// 		if err != nil {
		// 			log.Error(
		// 				fmt.Sprintf("%s something went wrong during new multiaddress validation: %w", fn, err),
		// 			)
		// 			continue
		// 		}

		// 		res = append(res, publicAddr)
		// 	}

		// 	return res
		// }),
		// libp2p.DisableIdentifyAddressDiscovery(),
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
			log.Error(
				fmt.Sprintf("Error during handling incoming stream: ", err),
			)
		}
	})

	go P.readBroker(ctx)
	go P.refreshConnections(ctx)

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

func (P *Peer) DeleteStreamToPeer(ID string) {
	const fn = "peer.DeleteStreamToPeer"

	stream := P.CheckStream(ID)

	P.StreamsMut.Lock()

	if stream != nil {
		err := stream.Stream.Reset()
		if err != nil {
			log.Error(
				fmt.Sprintf("%s: during stream close attempt: %w", fn, err),
			)
		}
	}

	delete(P.Streams, ID)

	P.StreamsMut.Unlock()
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
	var key []byte

	for {
		rawMessage, err := utils.ReadMessageWithLengthPrefix(s)
		if err != nil {
			log.Error(
				fmt.Sprintf("%s during reading message with prefix: %w", fn, err),
			)

			chat, err := P.Chats.GetChat(remoteUser.String())
			if err == nil {
				P.closeFailedConnection(chat)
			}

			return
		}

		if len(key) == 0 {
			key, err = P.getKeyToStream(remoteUser, rawMessage)
			if err != nil {
				log.Error(
					fmt.Sprintf("%s during getting key to stream: %w", fn, err),
				)
				return
			}

			chat, err := P.Chats.AddChat(fmt.Sprintf("%s/p2p/%s", s.Conn().RemoteMultiaddr().String(), remoteUser.String()))
			if err != nil {
				log.Error(
					fmt.Sprintf("%s during chat creation with remote multiaddress: %w", fn, err),
				)
				return
			}

			chat.SetConnStatus(defs.Connected)

			P.Broker.UpdateOnFront <- chat
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

	messageWithLengthPrefix, err := utils.AddLengthPrefixToMessage(message)
	if err != nil {
		return fmt.Errorf("%s: during message prefixing %w", fn, err)
	}

	_, err = s.Write(messageWithLengthPrefix)
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
		chat, err = P.Chats.AddChat(remoteUser)
		if err != nil {
			return fmt.Errorf("%s: %w", fn, err)
		}
	}

	if author != P.MyPeerID {
		chat.AppendMessage(author, message, defs.Received, P.Chats)
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
		isInit: make(chan struct{}, 1),
	}

	P.StreamsMut.Lock()
	P.Streams[s.Conn().RemotePeer().String()] = stream
	P.StreamsMut.Unlock()

	pubKeyWithPrefix, _ := utils.AddLengthPrefixToMessage(P.SessionPubKeys)

	_, err := s.Write(pubKeyWithPrefix)
	if err != nil {
		return nil, fmt.Errorf("%s during writing session public keys to stream: %w", fn, err)
	}

	go P.readFromStream(s)

	return stream, nil
}
