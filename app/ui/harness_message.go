package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/umputun/revdiff/app/ui/style"
)

// harnessMessageState owns the message draft independently of command history.
type harnessMessageState struct {
	active bool
	input  textinput.Model
	draft  string
	err    string
}

func (m Model) openHarnessMessage() (tea.Model, tea.Cmd) {
	if m.live.operation != liveIdle || m.live.discovery == discoveryForSend {
		m.output.hint = "Wait for the current review operation to finish"
		return m, nil
	}
	if m.live.content != "" && m.live.kind == feedbackAnnotations {
		m.output.hint = "Retry the unconfirmed annotations with O or :w before sending a message"
		return m, nil
	}
	m.clearPendingInputState()
	m.nav.scanSeq++
	m.nav.scanKind = treeScanIdle
	input := textinput.New()
	input.Prompt = "Message: "
	input.Placeholder = "Send a general message to the harness"
	input.CharLimit = annotCharLimit
	input.SetWidth(max(1, m.layout.width-4-len(input.Prompt)))
	input.SetValue(m.message.draft)
	if m.live.content != "" {
		input.SetValue(m.live.content)
	}
	focus := input.Focus()
	m.message = harnessMessageState{active: true, input: input, draft: m.message.draft}
	m.layout.viewport.SetHeight(m.paneHeight() - 1)
	model, discover := m.discoverFeedback(false)
	return model, tea.Batch(focus, discover)
}

func (m *Model) closeHarnessMessage() {
	m.message.draft = m.message.input.Value()
	m.message.active = false
	m.message.input.Blur()
	m.layout.viewport.SetHeight(m.paneHeight() - 1)
}

func (m *Model) updateHarnessMessageInput(msg tea.Msg) tea.Cmd {
	if m.live.content != "" {
		return nil // an unconfirmed delivery must retry its original text
	}
	before := m.message.input.Value()
	var cmd tea.Cmd
	m.message.input, cmd = m.message.input.Update(msg)
	if m.message.input.Value() != before {
		m.message.err = ""
	}
	return cmd
}

func (m Model) handleHarnessMessageKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeHarnessMessage()
		return m, nil
	case "enter":
		return m.sendHarnessMessage()
	default:
		cmd := m.updateHarnessMessageInput(msg)
		return m, cmd
	}
}

func (m Model) sendHarnessMessage() (tea.Model, tea.Cmd) {
	if m.live.operation != liveIdle {
		return m, nil
	}
	if m.live.sender == nil {
		m.message.err = "No harness connected. Close this box and use :harness connect <type>"
		return m, nil
	}
	if m.live.content == "" {
		content := m.message.input.Value()
		if strings.TrimSpace(content) == "" {
			m.message.err = "Enter a message to send"
			return m, nil
		}
		m.live.content, m.live.kind = content, feedbackMessage
	}
	m.message.err = ""
	return m.sendFeedback()
}

func (m Model) harnessMessageView() string {
	input := m.message.input
	styles := input.Styles()
	styles.Focused.Placeholder = m.resolver.Style(style.StyleKeyAnnotInputPlaceholder)
	input.SetStyles(styles)
	help := "Harness message · Enter send · Esc close (draft kept)"
	if m.live.content != "" {
		help = "Delivery unconfirmed · Enter retry · Esc close"
	}
	if m.message.err != "" {
		help = m.message.err
	}
	if m.live.operation == liveSending {
		help = "Sending message…"
	}
	return m.inputPaneView(input.View(), help)
}
