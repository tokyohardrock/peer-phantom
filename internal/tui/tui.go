package tui

import (
	"fmt"
	log "log/slog"
	"peer-phantom/internal/defs"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var docStyle = lipgloss.NewStyle().Margin(1, 1)

type ChatUpdateMsg struct {
	Chat *defs.ChatData
}

type sessionState int

const (
	screenList sessionState = iota
	screenChat
)

type chatModel struct {
	viewport    viewport.Model
	textarea    textarea.Model
	senderStyle lipgloss.Style

	selectedChat *defs.ChatData
}

func initialChatModel() chatModel {
	textarea := textarea.New()

	textarea.CharLimit = 280
	textarea.ShowLineNumbers = false
	textarea.Prompt = "┃ "
	textarea.Placeholder = "Type a message..."

	textarea.SetVirtualCursor(true)
	textarea.SetHeight(3)
	textarea.Focus()

	style := textarea.Styles()
	style.Focused.CursorLine = lipgloss.NewStyle()
	textarea.SetStyles(style)

	return chatModel{
		textarea:     textarea,
		viewport:     viewport.New(),
		senderStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		selectedChat: nil,
	}
}

func convertChatDataIntoListItem(items []*defs.ChatData) []list.Item {
	listItems := make([]list.Item, 0, len(items)+1)

	listItems = append(listItems, &defs.ChatData{
		ID: "Test Chat",
	})

	for _, item := range items {
		listItems = append(listItems, item)
	}

	return listItems
}

func initialListModel(items []list.Item) list.Model {
	list := list.New(items, list.NewDefaultDelegate(), 0, 0)

	list.Title = "PEER PHANTOM // CHATS"
	list.FilterInput.Prompt = "Search: "
	list.FilterInput.Placeholder = "Type multiaddress..."
	list.FilterInput.CharLimit = 0
	list.SetShowHelp(false)
	list.SetShowStatusBar(false)

	return list
}

type model struct {
	list list.Model
	chat chatModel

	chats  *defs.ChatStorage
	broker defs.Broker

	state sessionState

	peer []string
}

func (m model) Init() tea.Cmd {
	if m.state == screenChat {
		return tea.Batch(textarea.Blink, readBroker(m.broker))
	}

	return readBroker(m.broker)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	const fn = "tui.Update"

	var cmd tea.Cmd
	var cmds []tea.Cmd

	refreshChatView := func() {
		content := ""

		if m.chat.selectedChat != nil {
			content = strings.Join(m.chat.selectedChat.GetMessageSlice(), "\n")
		}

		m.chat.viewport.SetContent(
			lipgloss.
				NewStyle().
				Width(m.chat.viewport.Width()).
				Render(
					content,
				),
		)
		m.chat.viewport.GotoBottom()
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v-3)

		m.chat.viewport.SetWidth(msg.Width)
		m.chat.textarea.SetWidth(msg.Width)
		m.chat.viewport.SetHeight(msg.Height - m.chat.textarea.Height())
		refreshChatView()

		return m, nil
	case ChatUpdateMsg:
		// push received chat on top of the list
		list := m.chats.GetChatSlice()

		idx := slices.IndexFunc(list, func(chat *defs.ChatData) bool {
			return chat.ID == msg.Chat.ID
		})
		if idx != -1 {
			list = slices.Delete(list, idx, idx+1)
		}

		list = slices.Insert(list, 0, msg.Chat)

		// refresh chat viewport if received chat is selected
		if m.chat.selectedChat != nil && m.chat.selectedChat.ID == msg.Chat.ID {
			m.chat.selectedChat.MarkAsRead()
			refreshChatView()
		}

		return m, tea.Batch(
			m.list.SetItems(convertChatDataIntoListItem(list)),
			readBroker(m.broker),
		)
	}

	switch m.state {
	case screenList:
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "q":
				return m, tea.Quit
			case "enter":
				selectedItem := m.list.SelectedItem()

				if selectedItem == nil {
					newChat, err := m.chats.AddChat(m.list.FilterValue())
					if err != nil {
						log.Error(
							fmt.Sprintf("%s: %v", fn, err),
						)
						return m, nil
					}

					m.broker.UpdateOnBack <- newChat
					m.list.ResetFilter()

					return m, nil
				}

				m.chat.selectedChat = selectedItem.(*defs.ChatData)
				m.chat.selectedChat.MarkAsRead()
				m.state = screenChat

				refreshChatView()
				m.chat.textarea.Reset()

				return m, nil
			}
		}

		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	case screenChat:
		switch msg := msg.(type) {
		case tea.MouseWheelMsg:
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				m.chat.viewport.ScrollUp(1)
			case tea.MouseWheelDown:
				m.chat.viewport.ScrollDown(1)
			}
		case tea.KeyPressMsg:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.state = screenList
				m.chat.selectedChat = nil

				return m, nil
			case "enter":
				m.chat.selectedChat.AppendMessage("", m.chat.textarea.Value(), defs.Pending)
				m.broker.UpdateOnBack <- m.chat.selectedChat
				m.chat.textarea.Reset()

				return m, nil
			}
		}

		m.chat.textarea, cmd = m.chat.textarea.Update(msg)
		cmds = append(cmds, cmd)

		m.chat.viewport, cmd = m.chat.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() tea.View {
	var v tea.View

	switch m.state {
	case screenList:
		helpStr := "\n" + "↑/↓: navigate    /: search    Enter: open chat    Q: quit"
		addresses := "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
			Render(strings.Join(m.peer, "\n"))
		v = tea.NewView(docStyle.Render(m.list.View() + addresses + helpStr))
	case screenChat:
		viewportView := m.chat.viewport.View()
		content := viewportView + "\n" + m.chat.textarea.View()

		v = tea.NewView(content)
		v.MouseMode = tea.MouseModeCellMotion
	}

	v.AltScreen = true
	return v
}

func readBroker(broker defs.Broker) tea.Cmd {
	return func() tea.Msg {
		return ChatUpdateMsg{
			Chat: <-broker.UpdateOnFront,
		}
	}
}

func Run(chats defs.ChatStorage, broker defs.Broker) error {
	m := model{
		state:  screenList,
		list:   initialListModel(),
		chat:   initialChatModel(),
		chats:  chats,
		broker: broker,
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("Error running program: %w", err)
	}

	return nil
}
