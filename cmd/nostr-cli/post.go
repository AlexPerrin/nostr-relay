package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

type postModel struct {
	textarea  textarea.Model
	focused   bool
	status    string
	submitting bool

	relayURL string
	privKey  *btcec.PrivateKey
	width    int
	height   int
}

func newPostModel(relayURL string, privKey *btcec.PrivateKey) postModel {
	ta := textarea.New()
	ta.Placeholder = "What's on your mind?"
	ta.CharLimit = 4096
	ta.ShowLineNumbers = false
	ta.SetWidth(60)
	ta.SetHeight(6)

	return postModel{
		textarea: ta,
		relayURL: relayURL,
		privKey:  privKey,
	}
}

func (p postModel) Init() tea.Cmd { return nil }

func (p postModel) Update(msg tea.Msg) (postModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !p.focused {
			switch msg.String() {
			case "i", "enter":
				if p.privKey != nil {
					p.focused = true
					p.status = ""
					cmds = append(cmds, p.textarea.Focus())
				}
			}
			return p, tea.Batch(cmds...)
		}

		// Focused (editing) mode.
		switch msg.String() {
		case "esc":
			p.focused = false
			p.textarea.Blur()
			return p, nil

		case "ctrl+s":
			if p.submitting {
				return p, nil
			}
			content := strings.TrimSpace(p.textarea.Value())
			if content == "" {
				p.status = "content cannot be empty"
				return p, nil
			}
			p.submitting = true
			p.status = "publishing…"
			p.focused = false
			p.textarea.Blur()
			p.textarea.Reset()
			cmds = append(cmds, publishCmd(p.relayURL, p.privKey, content))
			return p, tea.Batch(cmds...)
		}
	}

	if p.focused {
		var cmd tea.Cmd
		p.textarea, cmd = p.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return p, tea.Batch(cmds...)
}

// publishCmd connects to the relay, signs and publishes the event, then disconnects.
func publishCmd(relayURL string, privKey *btcec.PrivateKey, content string) tea.Cmd {
	return func() tea.Msg {
		client, err := Connect(relayURL)
		if err != nil {
			return publishErrMsg{err}
		}
		defer client.Close()

		event := &Event{
			CreatedAt: time.Now().Unix(),
			Kind:      1,
			Tags:      [][]string{},
			Content:   content,
		}
		if err := SignEvent(event, privKey); err != nil {
			return publishErrMsg{err}
		}

		accepted, msg, err := client.Publish(event)
		if err != nil {
			return publishErrMsg{err}
		}
		return publishOKMsg{accepted: accepted, message: msg}
	}
}

func (p *postModel) setSize(width, height int) {
	p.width = width
	p.height = height

	taW := width - 6
	if taW < 10 {
		taW = 10
	}
	taH := height - 10
	if taH < 3 {
		taH = 3
	}
	p.textarea.SetWidth(taW)
	p.textarea.SetHeight(taH)
}

func (p postModel) View() string {
	var b strings.Builder
	b.WriteString("\n")

	if p.privKey == nil {
		b.WriteString(sectionStyle.Render(statusErrStyle.Render("No key configured — go to Profile and save your private key first.")))
		b.WriteString("\n")
		return b.String()
	}

	b.WriteString(sectionStyle.Render(labelStyle.Render("New Post")))
	b.WriteString("\n")
	b.WriteString(sectionStyle.Render(p.textarea.View()))
	b.WriteString("\n\n")

	b.WriteString(sectionStyle.Render(mutedStyle.Render(
		fmt.Sprintf("kind: 1  ·  relay: %s", p.relayURL),
	)))
	b.WriteString("\n\n")

	if p.status != "" {
		style := statusOKStyle
		if strings.HasPrefix(p.status, "error") ||
			strings.HasPrefix(p.status, "content") ||
			strings.HasPrefix(p.status, "rejected") {
			style = statusErrStyle
		}
		b.WriteString(sectionStyle.Render(style.Render(p.status)))
		b.WriteString("\n\n")
	}

	if p.focused {
		b.WriteString(hintStyle.Render("  [ctrl+s] publish  [esc] stop editing"))
	} else {
		b.WriteString(hintStyle.Render("  [i] write post"))
	}

	return b.String()
}
