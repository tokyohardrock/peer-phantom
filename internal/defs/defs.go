package defs

import (
	"fmt"
	"strings"
	"sync"
)

var ErrorNoChat = fmt.Errorf("no chat with this user")
var ErrorOutOfRange = fmt.Errorf("given index is out of range")

type MessageStatus int

const (
	Sent MessageStatus = iota
	Pending
	Received
	Error
)

type Message struct {
	Author  string
	Message string
	Status  MessageStatus

	Mutex *sync.RWMutex
}

type ChatData struct {
	RemoteUser  string
	UnreadCount int
	Messages    []*Message

	Mutex *sync.RWMutex
}

func InitChatData(RemoteUser string) *ChatData {
	return &ChatData{
		RemoteUser:  RemoteUser,
		UnreadCount: 0,
		Messages:    make([]*Message, 0, 100),
		Mutex:       &sync.RWMutex{},
	}
}

func (d ChatData) GetRemoteUser() string {
	d.Mutex.RLock()
	defer d.Mutex.RUnlock()

	return d.RemoteUser
}

func (d ChatData) GetUnreadCount() int {
	d.Mutex.RLock()
	defer d.Mutex.RUnlock()

	return d.UnreadCount
}

func (d ChatData) GetMessageSlice() []string {
	d.Mutex.RLock()
	defer d.Mutex.RUnlock()

	s := make([]string, 0, len(d.Messages))

	for _, m := range d.Messages {
		message := strings.Join([]string{m.Author, m.Message}, ": ")
		s = append(s, message)
	}

	return s
}

func (d ChatData) Title() string {
	if d.GetUnreadCount() > 0 {
		return fmt.Sprintf("%s (!)", d.RemoteUser)
	}

	return d.RemoteUser
}

func (d ChatData) Description() string {
	UnreadCount := d.GetUnreadCount()

	if UnreadCount > 0 {
		return fmt.Sprintf("%d new Messages", UnreadCount)
	}

	return "no new Messages"
}

func (d ChatData) FilterValue() string {
	return d.RemoteUser
}

func (d *ChatData) AppendMessage(author string, message string) {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.Messages = append(d.Messages, &Message{
		Author:  author,
		Message: message,
	})
}

func (d *ChatData) NewMessage() {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.UnreadCount++
}

type ChatStorage struct {
	Chats map[string]*ChatData
	Mutex *sync.RWMutex
}

func InitChatStorage() ChatStorage {
	cs := ChatStorage{
		Chats: make(map[string]*ChatData, 10),
		Mutex: &sync.RWMutex{},
	}

	return cs
}

func (s ChatStorage) GetChat(RemoteUser string) (*ChatData, error) {
	const fn = "defs.GetChatData"

	s.Mutex.RLock()
	data, ok := s.Chats[RemoteUser]
	s.Mutex.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%s: %w", fn, ErrorNoChat)
	}

	return data, nil
}

func (s ChatStorage) AddChat(RemoteUser string) *ChatData {
	chatData := InitChatData(RemoteUser)

	s.Mutex.Lock()
	s.Chats[RemoteUser] = chatData
	s.Mutex.Unlock()

	return chatData
}

func (s ChatStorage) GetChatSlice() []*ChatData {
	s.Mutex.RLock()
	defer s.Mutex.RUnlock()

	chatSlice := make([]*ChatData, len(s.Chats))
	for _, v := range s.Chats {
		chatSlice = append(chatSlice, v)
	}

	return chatSlice
}

// events from front (backend receives):
// - new chat created. need to connect
// - new message sent. need to write to stream
//
// events from back (frontend receives):
// - just need to know what chats are up to date so
// we can really just pass *ChatData and push it on top in list
// and in fullscreen chat just rerender the whole viewport

type Broker struct {
	UpdateOnFront chan *ChatData
	UpdateOnBack  chan *ChatData
}

func InitBroker() Broker {
	return Broker{
		UpdateOnFront: make(chan *ChatData, 5),
		UpdateOnBack:  make(chan *ChatData, 5),
	}
}
