package input

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"peer-phantom/internal/chat"
	"peer-phantom/internal/peer"
	"peer-phantom/internal/utils"
	"strings"
)

func commandHandler(ctx context.Context, log *slog.Logger, localPeer *peer.Peer, command []string) error {
	const fn = "input.commandHandler"

	switch command[0] {
	case "/conn":
		if len(command) == 1 {
			return fmt.Errorf("%s: no multiaddr provided", fn)
		}
		maddr := command[1]

		info, err := localPeer.ConnectToPeer(ctx, maddr)
		if err != nil {
			return fmt.Errorf("%s: failed to connect to peer: %w", fn, err)
		}

		localPeer.ActivePeerID.Store(info.ID.String())

		s, err := localPeer.GetStreamToPeer(ctx, log, info.ID)
		if err != nil {
			return fmt.Errorf("%s: failed to get stream to peer: %w", fn, err)
		}

		fmt.Println("Connected!")

		go chat.ShowChat(localPeer)

		err = localPeer.WriteToStream(s.Stream, utils.ConcatenateStrings(utils.GetShortPeerID(localPeer.MyPeerID), " joined the chat\n"))
		if err != nil {
			return fmt.Errorf("%s: failed to write to stream: %w", fn, err)
		}
	case "/back":
		if peerID := localPeer.ActivePeerID.Load().(string); peerID != "" {
			s := localPeer.CheckStream(peerID)
			if s == nil {
				return fmt.Errorf("%s: no active stream to send leave message", fn)
			}

			err := localPeer.WriteToStream(s.Stream, utils.ConcatenateStrings(utils.GetShortPeerID(localPeer.MyPeerID), " leaved the chat\n"))
			if err != nil {
				return fmt.Errorf("%s: failed to write to stream: %w", fn, err)
			}

			return errors.New("left the chat")
		} else {
			fmt.Println("Press Ctrl+C to exit")
		}
	default:
		fmt.Println("Unknown command!")
	}

	return nil
}

func inputHandler(ctx context.Context, log *slog.Logger, localPeer *peer.Peer, rawInput string) error {
	const fn = "input.inputHandler"

	words := strings.Fields(rawInput)

	if len(words) > 0 && rawInput[0] == '/' {
		err := commandHandler(ctx, log, localPeer, words)
		if err != nil {
			return fmt.Errorf("%s: %w", fn, err)
		}

		return nil
	}

	s := localPeer.CheckStream(localPeer.ActivePeerID.Load().(string))
	if s == nil {
		return fmt.Errorf("%s: no active stream to send message", fn)
	}

	err := localPeer.WriteToStream(s.Stream, utils.ConcatenateStrings(utils.GetShortPeerID(localPeer.MyPeerID), ": ", rawInput))

	return err
}

func ListenStdin(ctx context.Context, log *slog.Logger, localPeer *peer.Peer) {
	reader := bufio.NewReader(os.Stdin)

	for {
		localPeer.ActivePeerID.Store("")

		// terminal.Clear()

		fmt.Println("-=] Peer Phantom [=-")
		fmt.Println("Your addresses:")
		for i, addr := range localPeer.Host.Addrs() {
			fmt.Printf("%d. %s/p2p/%s\n", i+1, addr, localPeer.MyPeerID)
		}
		fmt.Println()

		fmt.Println("Avaliable commands: ")
		fmt.Println("\t/conn multiaddr")
		fmt.Println("\t/back")

		for {
			input, err := reader.ReadString('\n')
			if err != nil {
				log.Error(err.Error())
				continue
			}

			err = inputHandler(ctx, log, localPeer, input)
			if err != nil {
				log.Error(err.Error())
				break
			}
		}
	}
}
