package chat

import (
	"context"
	"fmt"
	"peer-phantom/internal/peer"
)

func ShowChat(ctx context.Context, localPeer *peer.Peer) {
	activeID := localPeer.ActivePeerID.Load().(string)

	localPeer.ChatsMut.RLock()
	chat := localPeer.Chats[activeID]
	for i := range chat {
		fmt.Print(chat[i].Message)
	}

	lastPrinted := len(chat)
	localPeer.ChatsMut.RUnlock()

	for {
		select {
		case <-ctx.Done():
			return
		case <-localPeer.NewMessage:
			activeID := localPeer.ActivePeerID.Load().(string)
			if activeID == "" {
				return
			}

			localPeer.ChatsMut.RLock()
			chatHistory := localPeer.Chats[activeID]

			if len(chatHistory) > lastPrinted {
				unread := chatHistory[lastPrinted:]

				for i := range unread {
					fmt.Print(unread[i].Message)
				}

				lastPrinted = len(chatHistory)
			}
			localPeer.ChatsMut.RUnlock()
		}
	}
}
