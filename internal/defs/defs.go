package defs

import (
	"fmt"
	"slices"
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

	Mutex sync.RWMutex
}

type ChatData struct {
	ID         string
	RemoteAddr string

	UnreadCount int
	Messages    []*Message

	Mutex sync.RWMutex
}

func getIDFromRemoteAddr(remoteAddr string) (string, error) {
	const fn = "defs.getIDFromRemoteAddr"

	_, id, ok := strings.Cut(remoteAddr, "/p2p/")
	if !ok {
		return "", fmt.Errorf("%s: invalid address", fn)
	}

	return id, nil
}

func InitChatData(remoteAddr string, id string) *ChatData {
	return &ChatData{
		ID:          id,
		RemoteAddr:  remoteAddr,
		UnreadCount: 0,
		Messages:    make([]*Message, 0, 100),
		Mutex:       sync.RWMutex{},
	}, nil
}

func (d *ChatData) GetChatID() string {
	d.Mutex.RLock()
	defer d.Mutex.RUnlock()

	return d.ID
}

func (d *ChatData) GetRemoteAddress() string {
	d.Mutex.RLock()
	defer d.Mutex.RUnlock()

	return d.RemoteAddr
}

func (d *ChatData) GetUnreadCount() int {
	d.Mutex.RLock()
	defer d.Mutex.RUnlock()

	return d.UnreadCount
}

func (d *ChatData) GetMessageSlice() []string {
	d.Mutex.RLock()
	defer d.Mutex.RUnlock()

	s := make([]string, 0, len(d.Messages))

	for _, m := range d.Messages {
		message := strings.Join([]string{m.Author, m.Message}, ": ")
		s = append(s, message)
	}

	return s
}

func (d *ChatData) Title() string {
	if d.GetUnreadCount() > 0 {
		return fmt.Sprintf("%s (!)", d.ID)
	}

	return d.ID
}

func (d *ChatData) Description() string {
	UnreadCount := d.GetUnreadCount()

	if UnreadCount > 0 {
		return fmt.Sprintf("%d new Messages", UnreadCount)
	}

	return "no new Messages"
}

func (d *ChatData) FilterValue() string {
	return d.ID
}

func (d *ChatData) AppendMessage(author string, message string, status MessageStatus, storage *ChatStorage) {
	d.Mutex.Lock()

	d.Messages = append(d.Messages, &Message{
		Author:  author,
		Message: message,
		Status:  status,
		Mutex:   sync.RWMutex{},
	})

	d.Mutex.Unlock()

	storage.PushChatOnTop(d)
}

func (d *ChatData) NewMessage() {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.UnreadCount++
}

func (d *ChatData) MarkAsRead() {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()

	d.UnreadCount = 0
}

type ChatStorage struct {
	ChatMap   map[string]*ChatData
	ChatSlice []*ChatData

	Mutex sync.RWMutex
}

func InitChatStorage() *ChatStorage {
	storage := &ChatStorage{
		ChatMap:   make(map[string]*ChatData, 10),
		ChatSlice: make([]*ChatData, 0, 10),
		Mutex:     sync.RWMutex{},
	}

	return storage
}

func (s *ChatStorage) GetChat(ID string) (*ChatData, error) {
	const fn = "defs.GetChatData"

	s.Mutex.RLock()
	data, ok := s.ChatMap[ID]
	s.Mutex.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%s: %w", fn, ErrorNoChat)
	}

	return data, nil
}

func (s *ChatStorage) AddChat(remoteAddr string) (*ChatData, error) {
	const fn = "defs.AddChat"

	chatData, err := InitChatData(remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fn, err)
	}

	s.Mutex.Lock()
	s.ChatMap[chatData.ID] = chatData
	s.Mutex.Unlock()

	s.PushChatOnTop(chatData)

	return chatData, nil
}

func (s *ChatStorage) GetChatSlice() []*ChatData {
	s.Mutex.RLock()
	defer s.Mutex.RUnlock()

	chatSlice := make([]*ChatData, len(s.ChatSlice))
	copy(chatSlice, s.ChatSlice)

	return chatSlice
}

func (s *ChatStorage) PushChatOnTop(targetChat *ChatData) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	idx := slices.IndexFunc(s.ChatSlice, func(chat *ChatData) bool {
		return chat.ID == targetChat.ID
	})
	if idx != -1 {
		s.ChatSlice = slices.Delete(s.ChatSlice, idx, idx+1)
	}

	s.ChatSlice = slices.Insert(s.ChatSlice, 0, targetChat)
}

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
