package input

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"peer-phantom/internal/chat"
	"peer-phantom/internal/peer"
	"peer-phantom/internal/utils"
	"strings"
)

func commandHandler(ctx context.Context, localPeer *peer.Peer, command []string) error {
	switch command[0] {
	case "/conn":
		if len(command) == 1 {
			return errors.New("Not enough arguments!")
		}
		maddr := command[1]

		info, err := localPeer.ConnectToPeer(ctx, maddr)
		if err != nil {
			return err
		}

		localPeer.ActivePeerID.Store(info.ID.String())

		s, err := localPeer.GetStreamToPeer(ctx, info.ID)
		if err != nil {
			return err
		}

		fmt.Println("Connected!")

		go chat.ShowChat(localPeer)

		err = localPeer.WriteToStream(s.Stream, utils.ConcatenateStrings(utils.GetShortPeerID(localPeer.MyPeerID), " joined the chat\n"))
		if err != nil {
			return err
		}
	case "/back":
		if peerID := localPeer.ActivePeerID.Load().(string); peerID != "" {
			s := localPeer.CheckStream(peerID)
			if s == nil {
				return errors.New("commandHandler: stream doesn't exist")
			}

			err := localPeer.WriteToStream(s.Stream, utils.ConcatenateStrings(utils.GetShortPeerID(localPeer.MyPeerID), " leaved the chat\n"))
			if err != nil {
				return err
			}

			return errors.New("Leaving chat ...")
		} else {
			fmt.Println("Press Ctrl+C to exit")
		}
	default:
		fmt.Println("Unknown command!")
	}

	return nil
}

func inputHandler(ctx context.Context, localPeer *peer.Peer, rawInput string) error {
	words := strings.Fields(rawInput)

	if len(words) > 0 && rawInput[0] == '/' {
		err := commandHandler(ctx, localPeer, words)
		if err != nil {
			return err
		}

		return nil
	}

	s := localPeer.CheckStream(localPeer.ActivePeerID.Load().(string))
	if s == nil {
		return errors.New("inputHandler: stream doesn't exist")
	}

	err := localPeer.WriteToStream(s.Stream, utils.ConcatenateStrings(utils.GetShortPeerID(localPeer.MyPeerID), ": ", rawInput))

	return err
}

func ListenStdin(ctx context.Context, localPeer *peer.Peer) {
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
				fmt.Println(err)
				continue
			}

			err = inputHandler(ctx, localPeer, input)
			if err != nil {
				fmt.Println(err)
				break
			}
		}
	}
}
