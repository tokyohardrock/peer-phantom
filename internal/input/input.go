package input

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"peer-phantom/internal/chat"
	"peer-phantom/internal/peer"
	"peer-phantom/internal/terminal"
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

		err = localPeer.GetStreamToPeer(ctx, info)
		if err != nil {
			return err
		}

		go chat.ShowChat(localPeer)
		localPeer.WriteToStream(utils.ConcatenateStrings(utils.GetShortPeerID(localPeer.MyPeerID), " joined the chat\n"))
	case "/back":
		if localPeer.ActivePeerID.Load().(string) != "" {
			localPeer.WriteToStream(utils.ConcatenateStrings(utils.GetShortPeerID(localPeer.MyPeerID), " leaved the chat\n"))
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

	localPeer.StreamsMut.RLock()

	if _, ok := localPeer.Streams[localPeer.ActivePeerID.Load().(string)]; !ok {
		localPeer.StreamsMut.RUnlock()
		return errors.New("Unable to find a stream for the specified peer")
	}

	localPeer.StreamsMut.RUnlock()

	err := localPeer.WriteToStream(utils.ConcatenateStrings(utils.GetShortPeerID(localPeer.MyPeerID), ": ", rawInput))

	return err
}

func ListenStdin(ctx context.Context, localPeer *peer.Peer) {
	reader := bufio.NewReader(os.Stdin)

	for {
		localPeer.ActivePeerID.Store("")

		terminal.Clear()

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
				log.Println(err)
				continue
			}

			err = inputHandler(ctx, localPeer, input)
			if err != nil {
				break
			}
		}
	}
}
