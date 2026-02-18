package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── feed-specific styles ─────────────────────────────────────────────────────

var (
	feedPKStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
	feedTSStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	feedKindStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("239"))
	feedDivStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	connDotOn     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("●")
	connDotOff    = lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("○")
)

// ── model ────────────────────────────────────────────────────────────────────

type feedModel struct {
	viewport viewport.Model
	ready    bool

	// events holds fully-rendered strings in chronological order (oldest→newest).
	events []string
	// pending buffers pre-EOSE stored events (relay sends newest-first).
	// They are sorted oldest-first and flushed into events on EOSE.
	pending      []*Event
	eoseReceived bool

	client     *Client
	eventCh    <-chan *Event
	connected  bool
	connecting bool
	status     string
	relayURL   string
}

func newFeedModel(relayURL string) feedModel {
	return feedModel{
		relayURL: relayURL,
		status:   "not connected — press [2] to connect",
	}
}

// ── commands ─────────────────────────────────────────────────────────────────

// connectCmd dials the relay, registers an EOSE callback, and opens a kind:1
// subscription. The returned feedConnectedMsg carries both channels.
func connectCmd(relayURL string) tea.Cmd {
	return func() tea.Msg {
		client, err := Connect(relayURL)
		if err != nil {
			return feedConnErrMsg{err}
		}

		// Buffer one EOSE signal. Register before Subscribe to avoid a race
		// where EOSE fires before the callback is set.
		eoseCh := make(chan struct{}, 1)
		client.OnEOSE("feed", func() {
			select {
			case eoseCh <- struct{}{}:
			default:
			}
		})

		ch := client.Subscribe("feed", Filter{Kinds: []int{1}})
		return feedConnectedMsg{client: client, eventCh: ch, eoseCh: eoseCh}
	}
}

// waitForEventCmd blocks until the next event arrives or the channel closes.
func waitForEventCmd(ch <-chan *Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return feedDisconnectedMsg{}
		}
		return relayEventMsg{event: event}
	}
}

// waitForEOSECmd blocks until the relay signals end-of-stored-events.
func waitForEOSECmd(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return feedEOSEMsg{}
	}
}

// ── update ───────────────────────────────────────────────────────────────────

func (f feedModel) Init() tea.Cmd { return nil }

func (f feedModel) Update(msg tea.Msg) (feedModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			if f.client != nil {
				f.client.Close()
			}
			f.connected = false
			f.connecting = true
			f.status = "reconnecting…"
			// Clear all state so reconnect starts with a clean feed.
			f.events = nil
			f.pending = nil
			f.eoseReceived = false
			if f.ready {
				f.viewport.SetContent(mutedStyle.Render("  No events yet."))
			}
			return f, connectCmd(f.relayURL)
		}
	}

	if f.ready {
		var cmd tea.Cmd
		f.viewport, cmd = f.viewport.Update(msg)
		return f, cmd
	}
	return f, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (f *feedModel) setSize(width, height int) {
	vpH := height - 3
	if vpH < 3 {
		vpH = 3
	}
	vpW := width - 2
	if vpW < 10 {
		vpW = 10
	}
	if !f.ready {
		f.viewport = viewport.New(vpW, vpH)
		f.viewport.SetContent(mutedStyle.Render("  No events yet."))
		f.ready = true
	} else {
		f.viewport.Width = vpW
		f.viewport.Height = vpH
	}
}

// addEvent is called for every event that arrives from the relay.
// Before EOSE it buffers; after EOSE it appends directly to the viewport.
func (f *feedModel) addEvent(e *Event) {
	if !f.eoseReceived {
		f.pending = append(f.pending, e)
		return
	}
	f.appendRendered(e)
}

// flushStoredEvents is called on EOSE. It sorts the buffered stored events
// oldest-first (inverse of the relay's newest-first delivery order), renders
// them into the viewport, then appends the live-events divider.
func (f *feedModel) flushStoredEvents() {
	f.eoseReceived = true

	if len(f.pending) > 0 {
		sort.Slice(f.pending, func(i, j int) bool {
			return f.pending[i].CreatedAt < f.pending[j].CreatedAt
		})
		for _, e := range f.pending {
			f.events = append(f.events, f.renderEvent(e))
		}
		f.pending = nil
	}

	// Divider separates stored events from live events.
	f.events = append(f.events, feedDivStyle.Render(
		"  "+strings.Repeat("·", 48)+"  live",
	))

	if f.ready {
		content := strings.Join(f.events, "\n")
		if len(f.events) == 1 { // only the divider, no stored events
			content = mutedStyle.Render("  No events yet.") + "\n" + content
		}
		f.viewport.SetContent(content)
		f.viewport.GotoBottom()
	}
}

// appendRendered renders a single event and appends it to the live section.
func (f *feedModel) appendRendered(e *Event) {
	f.events = append(f.events, f.renderEvent(e))
	if f.ready {
		f.viewport.SetContent(strings.Join(f.events, "\n"))
		f.viewport.GotoBottom()
	}
}

// renderEvent returns a formatted multi-line string for one event entry.
func (f *feedModel) renderEvent(e *Event) string {
	ts := time.Unix(e.CreatedAt, 0).Format("2006-01-02 15:04")
	pk := e.PubKey
	if len(pk) > 16 {
		pk = pk[:8] + "…" + pk[len(pk)-4:]
	}
	content := e.Content
	if maxW := f.viewport.Width - 4; maxW > 0 && len(content) > maxW*4 {
		content = content[:maxW*4] + "…"
	}
	return fmt.Sprintf("  %s  %s  %s\n  %s\n  %s",
		feedPKStyle.Render(pk),
		feedTSStyle.Render(ts),
		feedKindStyle.Render(fmt.Sprintf("kind:%d", e.Kind)),
		content,
		feedDivStyle.Render(strings.Repeat("─", 48)),
	)
}

// ── view ─────────────────────────────────────────────────────────────────────

func (f feedModel) View() string {
	dot := connDotOff
	if f.connected {
		dot = connDotOn
	}
	statusLine := sectionStyle.Render(fmt.Sprintf("%s  %s", dot, mutedStyle.Render(f.status)))

	var vpView string
	if f.ready {
		vpView = f.viewport.View()
	} else {
		vpView = mutedStyle.Render("  (waiting for terminal size…)")
	}

	hints := hintStyle.Render("  [↑/↓] scroll  [r] reconnect")
	return fmt.Sprintf("\n%s\n\n%s\n%s", statusLine, vpView, hints)
}
