package tui

import (
	"fmt"
	"peer-phantom/internal/defs"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var docStyle = lipgloss.NewStyle().Margin(1, 1)

type ChatUpdateMsg *defs.ChatData
type sessionState int

const (
	screenList sessionState = iota
	screenChat
)

type chatModel struct {
	viewport    viewport.Model
	textarea    textarea.Model
	senderStyle lipgloss.Style

	chatData *defs.ChatData
}

func initialChatModel() chatModel {
	ta := textarea.New()

	ta.CharLimit = 280
	ta.ShowLineNumbers = false
	ta.Prompt = "┃ "
	ta.Placeholder = "Type a message..."

	ta.SetVirtualCursor(true)
	ta.SetHeight(3)
	ta.Focus()

	s := ta.Styles()
	s.Focused.CursorLine = lipgloss.NewStyle()
	ta.SetStyles(s)

	return chatModel{
		textarea:    ta,
		viewport:    viewport.New(),
		senderStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		chatData:    nil,
	}
}

func initialListModel() list.Model {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)

	l.Title = "PEER PHANTOM // CHATS"
	l.FilterInput.Prompt = "Search: "
	l.FilterInput.Placeholder = "Type multiaddress..."
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)

	return l
}

type model struct {
	list list.Model
	chat chatModel

	chats  defs.ChatStorage
	broker defs.Broker

	state sessionState
}

func (m model) Init() tea.Cmd {
	if m.state == screenChat {
		return tea.Batch(textarea.Blink, readBroker(m.broker))
	}

	return readBroker(m.broker)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)

		m.chat.viewport.SetWidth(msg.Width)
		m.chat.textarea.SetWidth(msg.Width)
		m.chat.viewport.SetHeight(msg.Height - m.chat.textarea.Height())
		m.chat.viewport.SetContent(
			lipgloss.
				NewStyle().
				Width(m.chat.viewport.Width()).
				Render(
					strings.Join(m.chat.chatData.GetMessageSlice(), "\n"),
				),
		)
		m.chat.viewport.GotoBottom()
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
				if item := m.list.SelectedItem(); item == nil {
					m.list.ResetFilter()
				}
				m.state = screenChat

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
				return m, nil
			case "enter":
				m.chat.chatData.AppendMessage("You", m.chat.textarea.Value())
				m.chat.viewport.SetContent(
					lipgloss.
						NewStyle().
						Width(m.chat.viewport.Width()).
						Render(
							strings.Join(m.chat.chatData.GetMessageSlice(), "\n"),
						),
				)
				m.chat.textarea.Reset()
				m.chat.viewport.GotoBottom()

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
		innerView := m.list.View() + "\n" + "↑/↓: navigate    /: search    Enter: open chat    Q: quit"
		v = tea.NewView(docStyle.Render(innerView))
	case screenChat:
		viewportView := m.chat.viewport.View()
		v = tea.NewView(viewportView + "\n" + m.chat.textarea.View())
		v.MouseMode = tea.MouseModeCellMotion
	}

	v.AltScreen = true
	return v
}

func readBroker(broker defs.Broker) tea.Cmd {
	return func() tea.Msg {
		return ChatUpdateMsg(<-broker.UpdateOnFront)
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
