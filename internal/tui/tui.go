package tui

import (
	"fmt"
	"net"
	"encoding/json"

	"strings"
	"time"

	"devmux/internal/protocol"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type TUI struct {
	app   *tview.Application
	pages *tview.Pages
	addr  string
}

func NewTUI(addr string) (*TUI, error) {
	return &TUI{
		app:   tview.NewApplication(),
		pages: tview.NewPages(),
		addr:  addr,
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
	for _, line := range lines {
		name := strings.Split(line, ":")[0]
		tv := tview.NewTextView().
			SetDynamicColors(true).
			SetTextAlign(tview.AlignLeft).
			SetChangedFunc(func() {
				t.app.Draw()
			})
		tv.SetBorder(true).SetTitle(name)
		tv.ScrollToEnd()
		
		flex.AddItem(tv, 0, 1, false)
		go t.pollLogs(name, tv)
	}

	t.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC {
			t.app.Stop()
		}
		return event
	})

	t.app.SetRoot(flex, true).SetFocus(flex)
	return t.app.Run()
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
				t.app.Draw()
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
}
