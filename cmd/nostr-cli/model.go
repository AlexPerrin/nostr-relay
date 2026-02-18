package main

import (
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── tab enum ────────────────────────────────────────────────────────────────

type tab int

const (
	tabProfile tab = iota
	tabFeed
	tabPost
)

// ── shared message types ─────────────────────────────────────────────────────

type profileSavedMsg struct {
	relayURL string
	privKey  *btcec.PrivateKey
	pubKey   string
}

type profileErrMsg struct{ err error }

type feedConnectedMsg struct {
	client  *Client
	eventCh <-chan *Event
	eoseCh  <-chan struct{}
}

type feedConnErrMsg struct{ err error }

type relayEventMsg struct{ event *Event }

type feedEOSEMsg struct{}

type feedDisconnectedMsg struct{}

type publishOKMsg struct {
	accepted bool
	message  string
}

type publishErrMsg struct{ err error }

type keyGeneratedMsg struct {
	relayURL string
	privKey  *btcec.PrivateKey
	pubKey   string
	privHex  string
}

// ── shared styles ────────────────────────────────────────────────────────────

var (
	// Tab bar
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			Padding(0, 2).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("205"))

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Padding(0, 2)

	appTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("63")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")).
			Padding(0, 1)

	// Shared content styles used across screens
	labelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	valueStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	hintStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	statusOKStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	statusErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	sectionStyle   = lipgloss.NewStyle().Padding(0, 2)
)

// ── root model ───────────────────────────────────────────────────────────────

type model struct {
	tab    tab
	width  int
	height int

	// Shared state set by the Profile screen.
	relayURL string
	privKey  *btcec.PrivateKey
	pubKey   string

	profile profileModel
	feed    feedModel
	post    postModel
}

func initialModel() model {
	const defaultRelay = "ws://localhost:9090"
	return model{
		tab:      tabProfile,
		relayURL: defaultRelay,
		profile:  newProfileModel(defaultRelay),
		feed:     newFeedModel(defaultRelay),
		post:     newPostModel(defaultRelay, nil),
	}
}

func (m model) Init() tea.Cmd {
	return m.profile.Init()
}

// inputFocused reports whether a text field in the active screen owns input.
func (m model) inputFocused() bool {
	switch m.tab {
	case tabProfile:
		return m.profile.editing
	case tabPost:
		return m.post.focused
	}
	return false
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── window resize ─────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		contentH := msg.Height - 3 // header + footer
		m.profile.setSize(msg.Width, contentH)
		m.feed.setSize(msg.Width, contentH)
		m.post.setSize(msg.Width, contentH)
		return m, nil

	// ── global keys (only when no text field is focused) ─────────────────
	case tea.KeyMsg:
		if !m.inputFocused() {
			switch msg.String() {
			case "1":
				m.tab = tabProfile
				return m, m.profile.Init()
			case "2":
				m.tab = tabFeed
				if !m.feed.connected && !m.feed.connecting {
					m.feed.connecting = true
					m.feed.status = "connecting…"
					return m, connectCmd(m.feed.relayURL)
				}
				return m, nil
			case "3":
				m.tab = tabPost
				return m, m.post.Init()
			case "q", "ctrl+c":
				if m.feed.client != nil {
					m.feed.client.Close()
				}
				return m, tea.Quit
			}
		} else if msg.String() == "ctrl+c" {
			if m.feed.client != nil {
				m.feed.client.Close()
			}
			return m, tea.Quit
		}

	// ── profile results ───────────────────────────────────────────────────
	case profileSavedMsg:
		m.relayURL = msg.relayURL
		m.privKey = msg.privKey
		m.pubKey = msg.pubKey
		m.profile.pubKeyDisplay = msg.pubKey
		m.profile.status = "saved ✓"

		// Propagate new settings to other screens.
		m.post.relayURL = msg.relayURL
		m.post.privKey = msg.privKey

		if m.feed.relayURL != msg.relayURL {
			if m.feed.client != nil {
				m.feed.client.Close()
			}
			m.feed = newFeedModel(msg.relayURL)
			m.feed.setSize(m.width, m.height-3)
		}
		return m, nil

	case profileErrMsg:
		m.profile.status = "error: " + msg.err.Error()
		return m, nil

	case keyGeneratedMsg:
		m.relayURL = msg.relayURL
		m.privKey = msg.privKey
		m.pubKey = msg.pubKey
		// Populate the key input so the masked field reflects the new key.
		tmp := m.profile.inputs[fieldKey]
		tmp.SetValue(msg.privHex)
		m.profile.inputs[fieldKey] = tmp
		m.profile.pubKeyDisplay = msg.pubKey
		m.profile.generatedPrivHex = msg.privHex
		m.profile.status = "key generated ✓"
		m.post.relayURL = msg.relayURL
		m.post.privKey = msg.privKey
		return m, nil

	// ── feed results ──────────────────────────────────────────────────────
	case feedConnectedMsg:
		m.feed.client = msg.client
		m.feed.eventCh = msg.eventCh
		m.feed.connected = true
		m.feed.connecting = false
		m.feed.status = "connected · " + m.feed.relayURL
		cmds = append(cmds, waitForEventCmd(msg.eventCh))
		cmds = append(cmds, waitForEOSECmd(msg.eoseCh))
		return m, tea.Batch(cmds...)

	case feedConnErrMsg:
		m.feed.connected = false
		m.feed.connecting = false
		m.feed.status = "error: " + msg.err.Error()
		return m, nil

	case feedEOSEMsg:
		// Stored events have all arrived. Sort them oldest-first and flush to
		// the viewport, then append the live-events divider.
		m.feed.flushStoredEvents()
		return m, nil

	case relayEventMsg:
		m.feed.addEvent(msg.event)
		cmds = append(cmds, waitForEventCmd(m.feed.eventCh))
		return m, tea.Batch(cmds...)

	case feedDisconnectedMsg:
		// Guard against stale messages from a previous connection.
		if !m.feed.connected {
			m.feed.status = "disconnected · press [r] to reconnect"
		}
		return m, nil

	// ── post results ──────────────────────────────────────────────────────
	case publishOKMsg:
		m.post.submitting = false
		if msg.accepted {
			m.post.status = "published ✓"
		} else {
			m.post.status = "rejected: " + msg.message
		}
		return m, nil

	case publishErrMsg:
		m.post.submitting = false
		m.post.status = "error: " + msg.err.Error()
		return m, nil
	}

	// ── delegate remaining messages to active screen ──────────────────────
	switch m.tab {
	case tabProfile:
		var cmd tea.Cmd
		m.profile, cmd = m.profile.Update(msg)
		cmds = append(cmds, cmd)
	case tabFeed:
		var cmd tea.Cmd
		m.feed, cmd = m.feed.Update(msg)
		cmds = append(cmds, cmd)
	case tabPost:
		var cmd tea.Cmd
		m.post, cmd = m.post.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.width == 0 {
		return "initializing…"
	}

	// Tab bar
	labels := []string{"[1] Profile", "[2] Feed", "[3] Post"}
	tabs := make([]string, len(labels))
	for i, label := range labels {
		if tab(i) == m.tab {
			tabs[i] = activeTabStyle.Render(label)
		} else {
			tabs[i] = inactiveTabStyle.Render(label)
		}
	}
	header := lipgloss.JoinHorizontal(lipgloss.Bottom,
		appTitleStyle.Render("nostr-cli"),
		"  ",
		strings.Join(tabs, ""),
	)

	// Active screen content
	var content string
	switch m.tab {
	case tabProfile:
		content = m.profile.View()
	case tabFeed:
		content = m.feed.View()
	case tabPost:
		content = m.post.View()
	}

	footer := footerStyle.Render(fmt.Sprintf(
		"[1] profile  [2] feed  [3] post  [q] quit%s",
		func() string {
			if m.pubKey != "" {
				return "  · " + m.pubKey[:8] + "…"
			}
			return "  · no key"
		}(),
	))

	return fmt.Sprintf("%s\n%s\n%s", header, content, footer)
}
