package chat

import (
	"fmt"
	"peer-phantom/internal/peer"
	_ "peer-phantom/internal/terminal"
)

func ShowChat(localPeer *peer.Peer) {
	//terminal.Clear()

	localPeer.ChatsMut.RLock()

	snapshot := append([]peer.Mssg(nil), localPeer.Chats[localPeer.ActivePeerID.Load().(string)]...)
	lastPrinted := len(snapshot)

	localPeer.ChatsMut.RUnlock()

	for i := range snapshot {
		fmt.Print(snapshot[i].Message)
	}

	for {
		<-localPeer.NewMessage

		activePeerID := localPeer.ActivePeerID.Load().(string)

		if activePeerID == "" {
			return
		}

		localPeer.ChatsMut.RLock()

		unreadedMessages := append([]peer.Mssg(nil), localPeer.Chats[activePeerID][lastPrinted:]...)
		lastPrinted = len(localPeer.Chats[activePeerID])

		localPeer.ChatsMut.RUnlock()

		for i := range unreadedMessages {
			fmt.Print(unreadedMessages[i].Message)
		}
	}
}
