package tui

import (
	"fmt"
	log "log/slog"
	"peer-phantom/internal/defs"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
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
	screenInfo
	screenConnect
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
	listItems := make([]list.Item, 0, len(items))

	for _, item := range items {
		listItems = append(listItems, item)
	}

	return listItems
}

func initialListModel(items []list.Item) list.Model {
	delegate := list.NewDefaultDelegate()

	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("4")).
		BorderLeftForeground(lipgloss.Color("4"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color("8")).
		BorderLeftForeground(lipgloss.Color("4"))

	list := list.New(items, delegate, 0, 0)

	list.FilterInput.Prompt = "Search: "
	list.FilterInput.Placeholder = "Type multiaddress..."
	list.FilterInput.CharLimit = 0
	list.SetShowHelp(false)
	list.SetShowStatusBar(false)
	list.SetShowTitle(false)

	list.KeyMap.Quit.SetKeys()

	return list
}

func initialNewChatInput() textinput.Model {
	textinput := textinput.New()

	textinput.Placeholder = "/ip4/0.0.0.0/tcp/..."
	textinput.Prompt = "Enter multiaddress: "
	textinput.CharLimit = 150

	return textinput
}

type model struct {
	list         list.Model
	chat         chatModel
	newChatInput textinput.Model

	chats  *defs.ChatStorage
	broker defs.Broker

	state sessionState

	peer []string

	terminalHeight int
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
		m.terminalHeight = msg.Height

		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v-4) // subtract 4 to keep space for help str

		m.newChatInput.SetWidth(msg.Width - h - len(m.newChatInput.Prompt))

		m.chat.viewport.SetWidth(msg.Width)
		m.chat.textarea.SetWidth(msg.Width)
		m.chat.viewport.SetHeight(msg.Height - m.chat.textarea.Height())
		refreshChatView()

		return m, nil
	case ChatUpdateMsg:
		list := m.chats.GetChatSlice()

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
			case "ctrl+i":
				m.state = screenInfo
				return m, nil
			case "ctrl+n":
				m.state = screenConnect
				cmd = m.newChatInput.Focus()

				return m, cmd
			case "enter":
				selectedItem := m.list.SelectedItem()
				if selectedItem == nil {
					return m, nil
				}

				m.chat.selectedChat = selectedItem.(*defs.ChatData)
				m.chat.selectedChat.MarkAsRead()
				m.state = screenChat

				m.list.Select(0)

				refreshChatView()
				m.chat.textarea.Reset()

				return m, nil
			}
		}

		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
		case screenConnect:
			switch msg := msg.(type) {
			case tea.KeyPressMsg:
				switch msg.String() {
				case "ctrl+c":
					return m, tea.Quit
				case "esc":
					m.state = screenList
					m.newChatInput.Reset()

					return m, nil
				case "enter":
					address := strings.TrimSpace(m.newChatInput.Value())

					newChat, err := m.chats.AddChat(address)
					if err != nil {
						log.Error(
							fmt.Sprintf("%s (connect): %v", fn, err),
						)
						return m, nil
					}

					m.broker.UpdateOnBack <- newChat

					m.state = screenList
					m.newChatInput.Reset()

					return m, nil
				}
			}

			m.newChatInput, cmd = m.newChatInput.Update(msg)
			cmds = append(cmds, cmd)
	case screenInfo:
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.state = screenList
				return m, nil
			}
		}
	case screenChat:
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.state = screenList
				m.chat.selectedChat = nil

				return m, nil
			case "enter":
				m.chat.selectedChat.AppendMessage("You", m.chat.textarea.Value(), defs.Pending, m.chats)
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

func createPadding(content, terminalHeight int) string {
	paddingLines := terminalHeight - content
	padding := ""
	if paddingLines > 0 {
		padding = strings.Repeat("\n", paddingLines)
	}

	return padding
}

func (m model) View() tea.View {
	var v tea.View

	titleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("4")).
		Foreground(lipgloss.Color("0"))

	switch m.state {
	case screenList:
		title := titleStyle.Render(" PEER PHANTOM // CHATS ") + "\n\n"

		mainContent := docStyle.Render(title + m.list.View())
		helpStr := "↑/↓: navigate    /: search    Ctrl+N: new chat    Ctrl+I: info    Ctrl+C: quit"

		padding := createPadding(lipgloss.Height(mainContent)+lipgloss.Height(helpStr), m.terminalHeight)

		v = tea.NewView(mainContent + padding + helpStr)
	case screenConnect:
		title := titleStyle.Render(" PEER PHANTOM // NEW CHAT ") + "\n\n"
		inputView := m.newChatInput.View()

		mainContent := docStyle.Render(title + inputView)
		helpStr := "Enter: start chat    Esc: back"

		padding := createPadding(lipgloss.Height(mainContent)+lipgloss.Height(helpStr), m.terminalHeight)

		v = tea.NewView(mainContent + padding + helpStr)
	case screenInfo:
		title := titleStyle.Render(" PEER PHANTOM // INFO ") + "\n\n"

		subTitle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Render("Your addresses:") + "\n\n"

		addresses := lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")).
			Render(strings.Join(m.peer, "\n"))

		mainContent := docStyle.Render(title + subTitle + addresses)
		helpStr := "Esc: back"

		padding := createPadding(lipgloss.Height(mainContent)+lipgloss.Height(helpStr), m.terminalHeight)

		v = tea.NewView(mainContent + padding + helpStr)
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

func Run(chats *defs.ChatStorage, broker defs.Broker, myAddresses []string) error {
	m := model{
		state: screenList,
		list: initialListModel(
			convertChatDataIntoListItem(
				chats.GetChatSlice(),
			),
		),
		chat:         initialChatModel(),
		newChatInput: initialNewChatInput(),
		chats:        chats,
		broker:       broker,
		peer:         myAddresses,
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("Error running program: %w", err)
	}

	return nil
}
