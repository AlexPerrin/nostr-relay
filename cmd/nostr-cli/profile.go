package main

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// field indices
const (
	fieldRelay = 0
	fieldKey   = 1
)

type profileModel struct {
	inputs           [2]textinput.Model
	focused          int
	editing          bool
	status           string
	pubKeyDisplay    string
	generatedPrivHex string // shown after keygen so the user can copy it
	width            int
	height           int
}

func newProfileModel(relayURL string) profileModel {
	relay := textinput.New()
	relay.Placeholder = "ws://localhost:9090"
	relay.CharLimit = 256
	relay.SetValue(relayURL)

	key := textinput.New()
	key.Placeholder = "64-char hex private key"
	key.CharLimit = 64
	key.EchoMode = textinput.EchoPassword
	key.EchoCharacter = '•'

	return profileModel{
		inputs: [2]textinput.Model{relay, key},
	}
}

func (p profileModel) Init() tea.Cmd { return nil }

func (p profileModel) Update(msg tea.Msg) (profileModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !p.editing {
			switch msg.String() {
			case "e":
				p.editing = true
				p.status = ""
				p.inputs[p.focused].Focus()
				cmds = append(cmds, textinput.Blink)
			case "g":
				relayURL := strings.TrimSpace(p.inputs[fieldRelay].Value())
				if relayURL == "" {
					relayURL = "ws://localhost:9090"
				}
				return p, generateKeyCmd(relayURL)
			}
			return p, tea.Batch(cmds...)
		}

		// Editing mode key handling.
		switch msg.String() {
		case "esc":
			p.editing = false
			p.inputs[p.focused].Blur()

		case "tab", "down":
			p.inputs[p.focused].Blur()
			p.focused = (p.focused + 1) % 2
			p.inputs[p.focused].Focus()
			cmds = append(cmds, textinput.Blink)

		case "shift+tab", "up":
			p.inputs[p.focused].Blur()
			p.focused = (p.focused - 1 + 2) % 2
			p.inputs[p.focused].Focus()
			cmds = append(cmds, textinput.Blink)

		case "enter":
			if p.focused == fieldRelay {
				// Advance to the key field.
				p.inputs[p.focused].Blur()
				p.focused = fieldKey
				p.inputs[p.focused].Focus()
				cmds = append(cmds, textinput.Blink)
			} else {
				// Save on Enter in the key field.
				p.editing = false
				p.inputs[p.focused].Blur()
				cmds = append(cmds, saveProfileCmd(
					p.inputs[fieldRelay].Value(),
					p.inputs[fieldKey].Value(),
				))
			}

		default:
			var cmd tea.Cmd
			p.inputs[p.focused], cmd = p.inputs[p.focused].Update(msg)
			cmds = append(cmds, cmd)
		}

	default:
		// Forward cursor-blink and other internal messages to the focused input.
		if p.editing {
			var cmd tea.Cmd
			p.inputs[p.focused], cmd = p.inputs[p.focused].Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return p, tea.Batch(cmds...)
}

// saveProfileCmd validates inputs and produces a profileSavedMsg or profileErrMsg.
func saveProfileCmd(relayURL, keyHex string) tea.Cmd {
	return func() tea.Msg {
		relayURL = strings.TrimSpace(relayURL)
		keyHex = strings.TrimSpace(keyHex)

		if relayURL == "" {
			relayURL = "ws://localhost:9090"
		}
		if keyHex == "" {
			return profileErrMsg{fmt.Errorf("private key is required")}
		}

		privKey, err := parsePrivKey(keyHex)
		if err != nil {
			return profileErrMsg{err}
		}

		pubBytes := privKey.PubKey().SerializeCompressed()
		pubHex := hex.EncodeToString(pubBytes[1:]) // x-only pubkey

		return profileSavedMsg{
			relayURL: relayURL,
			privKey:  privKey,
			pubKey:   pubHex,
		}
	}
}

// generateKeyCmd creates a fresh secp256k1 keypair and returns a keyGeneratedMsg.
func generateKeyCmd(relayURL string) tea.Cmd {
	return func() tea.Msg {
		privKey, err := btcec.NewPrivateKey()
		if err != nil {
			return profileErrMsg{fmt.Errorf("keygen: %w", err)}
		}
		privHex := hex.EncodeToString(privKey.Serialize())
		pubBytes := privKey.PubKey().SerializeCompressed()
		pubHex := hex.EncodeToString(pubBytes[1:]) // x-only pubkey
		return keyGeneratedMsg{
			relayURL: relayURL,
			privKey:  privKey,
			pubKey:   pubHex,
			privHex:  privHex,
		}
	}
}

// parsePrivKey decodes a 64-char hex string into a secp256k1 private key.
func parsePrivKey(hexStr string) (*btcec.PrivateKey, error) {
	b, err := hex.DecodeString(strings.TrimSpace(hexStr))
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("expected 32 bytes (64 hex chars), got %d", len(b))
	}
	priv, _ := btcec.PrivKeyFromBytes(b)
	return priv, nil
}

func (p *profileModel) setSize(width, height int) {
	p.width = width
	p.height = height
	w := width - 6
	if w < 20 {
		w = 20
	}
	p.inputs[fieldRelay].Width = w
	p.inputs[fieldKey].Width = w
}

func (p profileModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(sectionStyle.Render(labelStyle.Render("Relay URL")))
	b.WriteString("\n")
	b.WriteString(sectionStyle.Render(p.inputs[fieldRelay].View()))
	b.WriteString("\n\n")

	b.WriteString(sectionStyle.Render(labelStyle.Render("Private Key (hex)")))
	b.WriteString("\n")
	b.WriteString(sectionStyle.Render(p.inputs[fieldKey].View()))
	b.WriteString("\n\n")

	if p.pubKeyDisplay != "" {
		b.WriteString(sectionStyle.Render(labelStyle.Render("Public Key")))
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render(valueStyle.Render(p.pubKeyDisplay)))
		b.WriteString("\n\n")
	}

	if p.generatedPrivHex != "" {
		warn := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
		b.WriteString(sectionStyle.Render(warn.Render("⚠  Back up your private key — it cannot be recovered:")))
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("215")).Render(p.generatedPrivHex),
		))
		b.WriteString("\n\n")
	}

	if p.status != "" {
		style := statusOKStyle
		if strings.HasPrefix(p.status, "error") {
			style = statusErrStyle
		}
		b.WriteString(sectionStyle.Render(style.Render(p.status)))
		b.WriteString("\n\n")
	}

	if p.editing {
		b.WriteString(hintStyle.Render("  [Tab] next field  [Enter] save  [Esc] cancel"))
	} else {
		if p.pubKeyDisplay == "" {
			b.WriteString(hintStyle.Render("  [g] generate key  [e] import key  ·  paste your 64-char hex private key to get started"))
		} else {
			b.WriteString(hintStyle.Render("  [g] generate new key  [e] edit"))
		}
	}

	return b.String()
}
