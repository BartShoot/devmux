package tui

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"devmux/internal/protocol"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type TUI struct {
	app        *tview.Application
	pages      *tview.Pages
	addr       string
	panes      []*tview.TextView
	focusIndex int
	autoScroll map[string]bool
	mu         sync.Mutex
}

func NewTUI(addr string) (*TUI, error) {
	return &TUI{
		app:        tview.NewApplication(),
		pages:      tview.NewPages(),
		addr:       addr,
		panes:      []*tview.TextView{},
		autoScroll: make(map[string]bool),
	}, nil
}

func (t *TUI) Run() error {
	// First, get the configuration/status from the daemon
	conn, err := net.Dial("tcp", t.addr)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer conn.Close()

	req := protocol.Request{Command: "status"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// Create a layout based on the response
	flex := tview.NewFlex().SetDirection(tview.FlexRow)

	// Create a TextView for each process
	lines := strings.Split(strings.TrimSpace(resp.Message), "\n")
	for i, line := range lines {
		name := strings.Split(line, ":")[0]
		t.autoScroll[name] = true

		tv := tview.NewTextView().
			SetDynamicColors(true).
			SetTextAlign(tview.AlignLeft).
			SetChangedFunc(func() {
				t.app.Draw()
			})
		tv.SetBorder(true).SetTitle(name)

		// Input capture for this specific pane
		tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			t.mu.Lock()
			defer t.mu.Unlock()

			switch event.Key() {
			case tcell.KeyUp, tcell.KeyPgUp:
				t.autoScroll[name] = false
			case tcell.KeyEnd:
				t.autoScroll[name] = true
				tv.ScrollToEnd()
			case tcell.KeyDown, tcell.KeyPgDn:
				// If we press Down/PgDn, we might want to re-enable autoscroll
				// In tview, it's hard to know if we hit the bottom, but the End key is reliable.
				// We can also just re-enable it on Down if we're feeling bold.
			}
			return event
		})

		t.panes = append(t.panes, tv)
		flex.AddItem(tv, 0, 1, i == 0)
		go t.pollLogs(name, tv)
	}

	t.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC {
			t.app.Stop()
		} else if event.Key() == tcell.KeyTab {
			// Cycle focus
			t.focusIndex = (t.focusIndex + 1) % len(t.panes)
			t.updateFocus()
			return nil // consume the event
		}
		return event
	})

	t.updateFocus()
	t.app.SetRoot(flex, true)
	return t.app.Run()
}

func (t *TUI) updateFocus() {
	for i, tv := range t.panes {
		if i == t.focusIndex {
			tv.SetBorderColor(tcell.ColorYellow)
			t.app.SetFocus(tv)
		} else {
			tv.SetBorderColor(tcell.ColorWhite)
		}
	}
}

func (t *TUI) pollLogs(name string, tv *tview.TextView) {
	offset := 0

	for {
		conn, err := net.DialTimeout("tcp", t.addr, 200*time.Millisecond)
		if err != nil {
			t.app.Stop()
			return
		}

		req := protocol.Request{Command: "logs", Name: name, Offset: offset}
		if err := json.NewEncoder(conn).Encode(req); err != nil {
			conn.Close()
			time.Sleep(1 * time.Second)
			continue
		}

		var resp protocol.Response
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			conn.Close()
			time.Sleep(1 * time.Second)
			continue
		}
		conn.Close()

		if resp.Status == "ok" {
			if offset > resp.TotalLines {
				// Buffer was cleared in daemon, clear it in TUI too
				tv.Clear()
				offset = 0
			}

			if resp.Message != "" {
				lines := strings.Split(resp.Message, "\n")
				for _, line := range lines {
					if line != "" {
						fmt.Fprintln(tv, line)
						offset++
					}
				}

				t.mu.Lock()
				shouldScroll := t.autoScroll[name]
				t.mu.Unlock()

				if shouldScroll {
					tv.ScrollToEnd()
				}
				t.app.Draw()
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
}
