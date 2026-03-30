package tui

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"devmux/internal/protocol"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ANSI color code to tview color tag mapping
var ansiToTview = map[string]string{
	"30": "black",
	"31": "red",
	"32": "green",
	"33": "yellow",
	"34": "blue",
	"35": "purple",
	"36": "cyan",
	"37": "white",
	"90": "gray",
	"91": "red",
	"92": "green",
	"93": "yellow",
	"94": "blue",
	"95": "purple",
	"96": "cyan",
	"97": "white",
}

// ansiRegex matches ANSI escape sequences
var ansiRegex = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// convertANSIToTview converts ANSI escape codes to tview color tags
func convertANSIToTview(s string) string {
	return ansiRegex.ReplaceAllStringFunc(s, func(match string) string {
		codes := ansiRegex.FindStringSubmatch(match)
		if len(codes) < 2 {
			return ""
		}

		parts := strings.Split(codes[1], ";")
		for _, code := range parts {
			if code == "0" || code == "39" || code == "" {
				return "[-]" // reset
			}
			if color, ok := ansiToTview[code]; ok {
				return "[" + color + "]"
			}
			// Bold (1) - tview uses [::b]
			if code == "1" {
				return "[::b]"
			}
		}
		return ""
	})
}

type TUI struct {
	app        *tview.Application
	pages      *tview.Pages
	tabBar     *tview.TextView
	network    string
	addr       string
	panes      map[string]*tview.TextView
	paneList   []*tview.TextView // ordered list for focus cycling
	focusIndex int
	autoScroll map[string]bool
	mu         sync.Mutex
	tabs       []string
	currentTab int
}

func NewTUI(network, addr string) (*TUI, error) {
	return &TUI{
		app:        tview.NewApplication(),
		pages:      tview.NewPages(),
		network:    network,
		addr:       addr,
		panes:      make(map[string]*tview.TextView),
		paneList:   []*tview.TextView{},
		autoScroll: make(map[string]bool),
		tabs:       []string{},
	}, nil
}

func (t *TUI) Run() error {
	// Get layout from daemon
	layout, err := t.fetchLayout()
	if err != nil {
		return fmt.Errorf("failed to get layout: %w", err)
	}

	if layout == nil || len(layout.Tabs) == 0 {
		return fmt.Errorf("no tabs defined in layout")
	}

	// Create tab bar
	t.tabBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	t.tabBar.SetBackgroundColor(tcell.ColorDarkBlue)

	// Build tabs
	for i, tab := range layout.Tabs {
		t.tabs = append(t.tabs, tab.Name)
		tabContent := t.buildTabContent(tab)
		t.pages.AddPage(tab.Name, tabContent, true, i == 0)
	}

	// Main layout: tab bar on top, content below
	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.tabBar, 1, 0, false).
		AddItem(t.pages, 0, 1, true)

	t.updateTabBar()

	// Global input handling
	t.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC:
			t.app.Stop()
			return nil
		case event.Key() == tcell.KeyTab:
			// Cycle focus within current tab
			if len(t.paneList) > 0 {
				t.focusIndex = (t.focusIndex + 1) % len(t.paneList)
				t.updateFocus()
			}
			return nil
		case event.Rune() == '1', event.Rune() == '2', event.Rune() == '3',
			event.Rune() == '4', event.Rune() == '5', event.Rune() == '6',
			event.Rune() == '7', event.Rune() == '8', event.Rune() == '9':
			// Switch tabs with number keys
			tabIndex := int(event.Rune() - '1')
			if tabIndex < len(t.tabs) {
				t.switchTab(tabIndex)
			}
			return nil
		case event.Key() == tcell.KeyLeft:
			// Previous tab
			if t.currentTab > 0 {
				t.switchTab(t.currentTab - 1)
			}
			return nil
		case event.Key() == tcell.KeyRight:
			// Next tab
			if t.currentTab < len(t.tabs)-1 {
				t.switchTab(t.currentTab + 1)
			}
			return nil
		}
		return event
	})

	// Start log polling for all panes
	for name, tv := range t.panes {
		go t.pollLogs(name, tv)
	}

	t.app.SetRoot(mainFlex, true)
	if len(t.paneList) > 0 {
		t.app.SetFocus(t.paneList[0])
	}

	return t.app.Run()
}

func (t *TUI) fetchLayout() (*protocol.Layout, error) {
	conn, err := net.Dial(t.network, t.addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := protocol.Request{Command: "layout"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}

	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}

	return resp.Layout, nil
}

func (t *TUI) buildTabContent(tab protocol.TabLayout) tview.Primitive {
	if len(tab.Panes) == 0 {
		return tview.NewBox()
	}

	// Create TextViews for each pane
	var paneViews []*tview.TextView
	for _, pane := range tab.Panes {
		tv := t.createPaneView(pane.Name)
		paneViews = append(paneViews, tv)
		t.panes[pane.Name] = tv
		t.paneList = append(t.paneList, tv)
		t.autoScroll[pane.Name] = true
	}

	// Apply layout
	layout := strings.ToLower(tab.Layout)
	if layout == "" {
		layout = "vertical"
	}

	switch layout {
	case "horizontal":
		// All panes side by side
		flex := tview.NewFlex().SetDirection(tview.FlexColumn)
		for _, tv := range paneViews {
			flex.AddItem(tv, 0, 1, false)
		}
		return flex

	case "split":
		// First pane on left, rest stacked on right
		if len(paneViews) == 1 {
			return paneViews[0]
		}
		leftPane := paneViews[0]
		rightFlex := tview.NewFlex().SetDirection(tview.FlexRow)
		for _, tv := range paneViews[1:] {
			rightFlex.AddItem(tv, 0, 1, false)
		}
		mainFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(leftPane, 0, 1, false).
			AddItem(rightFlex, 0, 1, false)
		return mainFlex

	default: // vertical
		flex := tview.NewFlex().SetDirection(tview.FlexRow)
		for _, tv := range paneViews {
			flex.AddItem(tv, 0, 1, false)
		}
		return flex
	}
}

func (t *TUI) createPaneView(name string) *tview.TextView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetTextAlign(tview.AlignLeft).
		SetChangedFunc(func() {
			t.app.Draw()
		})
	tv.SetBorder(true).SetTitle(" " + name + " ")

	// Input capture for scrolling
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
			// Keep scrolling, don't change autoscroll
		}
		return event
	})

	return tv
}

func (t *TUI) switchTab(index int) {
	if index < 0 || index >= len(t.tabs) {
		return
	}
	t.currentTab = index
	t.pages.SwitchToPage(t.tabs[index])
	t.updateTabBar()

	// Reset focus to first pane in new tab
	t.focusIndex = 0
	t.updateFocus()
}

func (t *TUI) updateTabBar() {
	var parts []string
	for i, name := range t.tabs {
		if i == t.currentTab {
			parts = append(parts, fmt.Sprintf("[black:white] %d:%s [-:-]", i+1, name))
		} else {
			parts = append(parts, fmt.Sprintf(" %d:%s ", i+1, name))
		}
	}
	t.tabBar.SetText(strings.Join(parts, "│") + "  [gray](←/→ or 1-9 to switch, Tab to cycle panes, q to quit)[-]")
}

func (t *TUI) updateFocus() {
	for i, tv := range t.paneList {
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
	dialer := &net.Dialer{Timeout: 200 * time.Millisecond}

	for {
		conn, err := dialer.Dial(t.network, t.addr)
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
				// Buffer was cleared, reset
				tv.Clear()
				offset = 0
			}

			if resp.Message != "" {
				lines := strings.Split(resp.Message, "\n")
				for _, line := range lines {
					if line != "" {
						// Convert ANSI codes to tview tags
						converted := convertANSIToTview(line)
						fmt.Fprintln(tv, converted)
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
