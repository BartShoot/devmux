package tui

import (
	"fmt"
	"strings"
	"sync"

	"devmux/internal/protocol"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// InputMode represents the current vim-like input mode
type InputMode uint8

const (
	ModeNormal InputMode = iota
	ModeInsert
)

// StreamingTUI is the new TUI using push-based streaming protocol
type StreamingTUI struct {
	app       *tview.Application
	pages     *tview.Pages
	tabBar    *tview.TextView
	statusBar *tview.TextView
	client    *StreamClient
	network   string
	addr      string

	// Input mode
	mode InputMode

	// Pane management (global registry)
	panes map[protocol.PaneID]*SimpleTerminalView

	// Per-tab pane views (ordered), built during layout
	tabPaneViews [][]*SimpleTerminalView

	// Focus: index within current tab's panes
	focusIndex int

	// Tab management
	tabs       []tabState
	currentTab int

	mu sync.Mutex
}

type tabState struct {
	id    protocol.TabID
	name  string
	panes []protocol.PaneID
}

// NewStreamingTUI creates a new streaming TUI
func NewStreamingTUI(network, addr string) *StreamingTUI {
	return &StreamingTUI{
		app:     tview.NewApplication(),
		pages:   tview.NewPages(),
		network: network,
		addr:    addr,
		panes:   make(map[protocol.PaneID]*SimpleTerminalView),
		mode:    ModeNormal,
	}
}

// Run starts the streaming TUI
func (t *StreamingTUI) Run() error {
	// Create stream client with handlers
	client, err := NewStreamClient(t.network, t.addr, StreamHandlers{
		OnLayout:       t.handleLayout,
		OnScreenUpdate: t.handleScreenUpdate,
		OnSelection:    t.handleSelection,
		OnPaneStatus:   t.handlePaneStatus,
		OnError:        t.handleError,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	t.client = client
	defer client.Close()

	// Start receive loop in background
	go func() {
		if err := client.ReceiveLoop(); err != nil {
			t.app.QueueUpdate(func() {
				t.app.Stop()
			})
		}
	}()

	// Request initial layout
	if err := client.SendGetLayout(); err != nil {
		return fmt.Errorf("failed to request layout: %w", err)
	}

	// Create tab bar
	t.tabBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	t.tabBar.SetBackgroundColor(colMantle)
	t.tabBar.SetTextColor(colText)

	// Create status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	t.statusBar.SetBackgroundColor(colMantle)
	t.statusBar.SetTextColor(colText)
	t.updateStatusBar()

	// Set background on pages container
	t.pages.SetBackgroundColor(colBase)

	// Main layout: tab bar on top, content in middle, status bar on bottom
	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.tabBar, 1, 0, false).
		AddItem(t.pages, 0, 1, true).
		AddItem(t.statusBar, 1, 0, false)
	mainFlex.SetBackgroundColor(colBase)

	// Global input handling
	t.app.SetInputCapture(t.handleInput)

	// After each draw, check if any pane sizes changed and send resize messages.
	// This runs after tview has computed actual layout dimensions.
	t.app.SetAfterDrawFunc(func(screen tcell.Screen) {
		t.mu.Lock()
		for paneID, tv := range t.panes {
			width, height := tv.GetInnerSize()
			if width > 0 && height > 0 {
				if tv.viewCols != width || tv.viewRows != height {
					tv.viewCols = width
					tv.viewRows = height
					go t.client.SendResize(paneID, width, height)
				}
			}
		}
		t.mu.Unlock()
	})

	t.app.SetRoot(mainFlex, true)
	return t.app.Run()
}

// handleLayout processes layout messages from daemon
func (t *StreamingTUI) handleLayout(layout *protocol.LayoutMsg) {
	t.app.QueueUpdateDraw(func() {
		t.mu.Lock()
		defer t.mu.Unlock()

		// Clear existing state
		t.tabs = nil
		t.tabPaneViews = nil
		t.panes = make(map[protocol.PaneID]*SimpleTerminalView)

		// Build tabs
		for _, tab := range layout.Tabs {
			var tabViews []*SimpleTerminalView
			tabContent := t.buildTabContent(tab, &tabViews)
			t.pages.AddPage(tab.Name, tabContent, true, len(t.tabs) == 0)

			paneIDs := make([]protocol.PaneID, len(tab.Panes))
			for i, p := range tab.Panes {
				paneIDs[i] = p.ID
			}
			t.tabs = append(t.tabs, tabState{
				id:    tab.ID,
				name:  tab.Name,
				panes: paneIDs,
			})
			t.tabPaneViews = append(t.tabPaneViews, tabViews)
		}

		t.updateTabBar()

		// Set focus to first pane in first tab
		if len(t.tabPaneViews) > 0 && len(t.tabPaneViews[0]) > 0 {
			t.focusIndex = 0
			t.updateFocus()
		}

		// Subscribe to all panes
		t.subscribeToAllPanes()
	})
}

// buildTabContent creates the layout for a tab and collects pane views
func (t *StreamingTUI) buildTabContent(tab protocol.TabInfo, tabViews *[]*SimpleTerminalView) tview.Primitive {
	if len(tab.Panes) == 0 {
		return tview.NewBox()
	}

	// Create views for each pane
	for _, pane := range tab.Panes {
		tv := NewSimpleTerminalView(pane.ID, pane.Name)
		tv.command = pane.Command
		tv.running = pane.Running
		title := formatPaneTitle(pane.Name, pane.Running, pane.Status)
		tv.SetTitle(title)
		*tabViews = append(*tabViews, tv)
		t.panes[pane.ID] = tv
	}

	// Apply layout
	layout := strings.ToLower(tab.Layout)
	if layout == "" {
		layout = "vertical"
	}

	switch layout {
	case "horizontal":
		flex := tview.NewFlex().SetDirection(tview.FlexColumn)
		for _, tv := range *tabViews {
			flex.AddItem(tv, 0, 1, false)
		}
		return flex

	case "split":
		if len(*tabViews) == 1 {
			return (*tabViews)[0]
		}
		leftPane := (*tabViews)[0]
		rightFlex := tview.NewFlex().SetDirection(tview.FlexRow)
		for _, tv := range (*tabViews)[1:] {
			rightFlex.AddItem(tv, 0, 1, false)
		}
		mainFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(leftPane, 0, 1, false).
			AddItem(rightFlex, 0, 1, false)
		return mainFlex

	default: // vertical
		flex := tview.NewFlex().SetDirection(tview.FlexRow)
		for _, tv := range *tabViews {
			flex.AddItem(tv, 0, 1, false)
		}
		return flex
	}
}

// subscribeToAllPanes subscribes to updates for all panes
func (t *StreamingTUI) subscribeToAllPanes() {
	var paneIDs []protocol.PaneID
	for id := range t.panes {
		paneIDs = append(paneIDs, id)
	}
	if len(paneIDs) > 0 {
		t.client.SendSubscribe(paneIDs)
	}

	// Mark all panes as needing initial resize.
	// The BeforeDrawFunc will pick these up on the first actual draw
	// when tview has computed real sizes.
	for _, tv := range t.panes {
		tv.viewCols = 0
		tv.viewRows = 0
	}
}

// handleScreenUpdate processes screen updates from daemon
func (t *StreamingTUI) handleScreenUpdate(update *protocol.ScreenUpdate) {
	t.mu.Lock()
	tv, ok := t.panes[update.PaneID]
	t.mu.Unlock()

	if ok {
		tv.UpdateScreen(update)
		t.app.QueueUpdateDraw(func() {})
	}
}

// handleSelection processes selection updates from daemon
func (t *StreamingTUI) handleSelection(sel *protocol.SelectionMsg) {
	t.mu.Lock()
	tv, ok := t.panes[sel.PaneID]
	t.mu.Unlock()

	if ok {
		tv.UpdateSelection(sel)
		t.app.QueueUpdateDraw(func() {})
	}
}

// handlePaneStatus processes pane status updates
func (t *StreamingTUI) handlePaneStatus(status *protocol.PaneStatusMsg) {
	t.mu.Lock()
	tv, ok := t.panes[status.PaneID]
	t.mu.Unlock()

	if ok {
		tv.mu.Lock()
		tv.running = status.Running
		tv.mu.Unlock()

		t.app.QueueUpdateDraw(func() {
			title := formatPaneTitle(tv.name, status.Running, status.Status)
			tv.SetTitle(title)
		})
	}
}

// handleError processes error messages
func (t *StreamingTUI) handleError(err *protocol.ErrorMsg) {
	fmt.Printf("Error from daemon: %s\n", err.Message)
}

// currentTabPanes returns the pane views for the currently active tab
func (t *StreamingTUI) currentTabPanes() []*SimpleTerminalView {
	if t.currentTab < len(t.tabPaneViews) {
		return t.tabPaneViews[t.currentTab]
	}
	return nil
}

// handleInput processes keyboard input based on current mode
func (t *StreamingTUI) handleInput(event *tcell.EventKey) *tcell.EventKey {
	t.mu.Lock()
	mode := t.mode
	t.mu.Unlock()

	switch mode {
	case ModeNormal:
		return t.handleNormalMode(event)
	case ModeInsert:
		return t.handleInsertMode(event)
	}
	return event
}

// handleNormalMode processes input in normal mode
func (t *StreamingTUI) handleNormalMode(event *tcell.EventKey) *tcell.EventKey {
	switch {
	// Quit
	case event.Rune() == 'q':
		t.app.Stop()
		return nil

	// Enter insert mode
	case event.Rune() == 'i':
		t.mu.Lock()
		t.mode = ModeInsert
		t.mu.Unlock()
		t.updateStatusBar()
		return nil

	// Process control
	case event.Key() == tcell.KeyCtrlC:
		t.processControlFocused(protocol.ProcessStop)
		return nil

	case event.Key() == tcell.KeyEnter:
		// Only start if process is stopped
		t.mu.Lock()
		panes := t.currentTabPanes()
		var stopped bool
		if t.focusIndex < len(panes) {
			panes[t.focusIndex].mu.RLock()
			stopped = !panes[t.focusIndex].running
			panes[t.focusIndex].mu.RUnlock()
		}
		t.mu.Unlock()
		if stopped {
			t.processControlFocused(protocol.ProcessStart)
		}
		return nil

	case event.Key() == tcell.KeyCtrlR:
		t.processControlFocused(protocol.ProcessRestart)
		return nil

	// Scrolling
	case event.Key() == tcell.KeyCtrlU:
		t.scrollFocused(protocol.ScrollUp, int16(t.focusedPaneHeight()/2))
		return nil

	case event.Key() == tcell.KeyCtrlD:
		t.scrollFocused(protocol.ScrollDown, int16(t.focusedPaneHeight()/2))
		return nil

	case event.Key() == tcell.KeyCtrlB:
		t.scrollFocused(protocol.ScrollUp, int16(t.focusedPaneHeight()))
		return nil

	case event.Key() == tcell.KeyCtrlF:
		t.scrollFocused(protocol.ScrollDown, int16(t.focusedPaneHeight()))
		return nil

	case event.Rune() == 'G':
		t.scrollFocused(protocol.ScrollTop, 0)
		return nil

	case event.Rune() == 'g':
		t.scrollFocused(protocol.ScrollBottom, 0)
		return nil

	// Pane navigation (within current tab)
	case event.Rune() == 'h' || event.Rune() == 'k':
		t.mu.Lock()
		panes := t.currentTabPanes()
		if len(panes) > 0 {
			t.focusIndex = (t.focusIndex - 1 + len(panes)) % len(panes)
			t.updateFocus()
		}
		t.mu.Unlock()
		return nil

	case event.Rune() == 'j' || event.Rune() == 'l':
		t.mu.Lock()
		panes := t.currentTabPanes()
		if len(panes) > 0 {
			t.focusIndex = (t.focusIndex + 1) % len(panes)
			t.updateFocus()
		}
		t.mu.Unlock()
		return nil

	case event.Key() == tcell.KeyTab:
		t.mu.Lock()
		panes := t.currentTabPanes()
		if len(panes) > 0 {
			t.focusIndex = (t.focusIndex + 1) % len(panes)
			t.updateFocus()
		}
		t.mu.Unlock()
		return nil

	// Tab switching with number keys
	case event.Rune() >= '1' && event.Rune() <= '9':
		tabIndex := int(event.Rune() - '1')
		t.switchTab(tabIndex)
		return nil

	// Tab switching with arrow keys
	case event.Key() == tcell.KeyLeft:
		t.mu.Lock()
		idx := t.currentTab
		t.mu.Unlock()
		if idx > 0 {
			t.switchTab(idx - 1)
		}
		return nil

	case event.Key() == tcell.KeyRight:
		t.mu.Lock()
		idx := t.currentTab
		t.mu.Unlock()
		if idx < len(t.tabs)-1 {
			t.switchTab(idx + 1)
		}
		return nil
	}

	return event
}

// scrollFocused sends a scroll command for the focused pane
func (t *StreamingTUI) scrollFocused(action protocol.ScrollAction, amount int16) {
	t.mu.Lock()
	panes := t.currentTabPanes()
	var paneID protocol.PaneID
	if t.focusIndex < len(panes) {
		paneID = panes[t.focusIndex].PaneID()
	}
	t.mu.Unlock()
	if paneID != 0 {
		go t.client.SendScroll(paneID, action, amount)
	}
}

// focusedPaneHeight returns the inner height of the focused pane
func (t *StreamingTUI) focusedPaneHeight() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	panes := t.currentTabPanes()
	if t.focusIndex < len(panes) {
		_, h := panes[t.focusIndex].GetInnerSize()
		if h > 0 {
			return h
		}
	}
	return 24
}

// processControlFocused sends a process control command for the focused pane
func (t *StreamingTUI) processControlFocused(action protocol.ProcessAction) {
	t.mu.Lock()
	panes := t.currentTabPanes()
	var paneID protocol.PaneID
	if t.focusIndex < len(panes) {
		paneID = panes[t.focusIndex].PaneID()
	}
	t.mu.Unlock()
	if paneID != 0 {
		go t.client.SendProcessControl(paneID, action)
	}
}

// handleInsertMode captures all input and forwards to the focused pane
func (t *StreamingTUI) handleInsertMode(event *tcell.EventKey) *tcell.EventKey {
	// Escape returns to normal mode
	if event.Key() == tcell.KeyEscape {
		t.mu.Lock()
		t.mode = ModeNormal
		t.mu.Unlock()
		t.updateStatusBar()
		return nil
	}

	// Forward everything to the focused pane
	t.mu.Lock()
	panes := t.currentTabPanes()
	var paneID protocol.PaneID
	if t.focusIndex < len(panes) {
		paneID = panes[t.focusIndex].PaneID()
	}
	t.mu.Unlock()

	if paneID == 0 {
		return nil
	}

	var input string
	switch event.Key() {
	case tcell.KeyEnter:
		input = "\r"
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		input = "\x7f"
	case tcell.KeyTab:
		input = "\t"
	case tcell.KeyCtrlC:
		input = "\x03"
	case tcell.KeyCtrlD:
		input = "\x04"
	case tcell.KeyCtrlR:
		input = "\x12"
	case tcell.KeyCtrlA:
		input = "\x01"
	case tcell.KeyCtrlE:
		input = "\x05"
	case tcell.KeyCtrlK:
		input = "\x0b"
	case tcell.KeyCtrlU:
		input = "\x15"
	case tcell.KeyCtrlW:
		input = "\x17"
	case tcell.KeyCtrlL:
		input = "\x0c"
	case tcell.KeyCtrlZ:
		input = "\x1a"
	case tcell.KeyUp:
		input = "\x1b[A"
	case tcell.KeyDown:
		input = "\x1b[B"
	case tcell.KeyRight:
		input = "\x1b[C"
	case tcell.KeyLeft:
		input = "\x1b[D"
	case tcell.KeyHome:
		input = "\x1b[H"
	case tcell.KeyEnd:
		input = "\x1b[F"
	case tcell.KeyDelete:
		input = "\x1b[3~"
	case tcell.KeyPgUp:
		input = "\x1b[5~"
	case tcell.KeyPgDn:
		input = "\x1b[6~"
	case tcell.KeyRune:
		input = string(event.Rune())
	}

	if input != "" {
		go t.client.SendInput(paneID, input)
	}
	return nil
}

// switchTab switches to the specified tab
func (t *StreamingTUI) switchTab(index int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if index < 0 || index >= len(t.tabs) {
		return
	}

	t.currentTab = index
	t.pages.SwitchToPage(t.tabs[index].name)
	t.updateTabBar()

	// Reset focus to first pane in new tab
	t.focusIndex = 0
	t.updateFocus()

	// Force resize for panes in the new tab — they may not have been
	// drawn yet, so schedule it after the next draw calculates sizes.
	go func() {
		t.app.QueueUpdateDraw(func() {
			t.mu.Lock()
			panes := t.currentTabPanes()
			for _, tv := range panes {
				width, height := tv.GetInnerSize()
				if width > 0 && height > 0 {
					tv.viewCols = width
					tv.viewRows = height
					go t.client.SendResize(tv.paneID, width, height)
				}
			}
			t.mu.Unlock()
		})
	}()
}

// updateTabBar updates the tab bar display
func (t *StreamingTUI) updateTabBar() {
	var parts []string
	for i, tab := range t.tabs {
		if i == t.currentTab {
			parts = append(parts, fmt.Sprintf("[#1e1e2e:#b4befe] %d:%s [-:-]", i+1, tab.name))
		} else {
			parts = append(parts, fmt.Sprintf("[#a6adc8:-] %d:%s [-:-]", i+1, tab.name))
		}
	}
	t.tabBar.SetText(strings.Join(parts, "[#6c7086]|[-]"))
}

// updateStatusBar updates the status bar text with current mode.
// Safe to call before or after app.Run — writes directly to the TextView.
func (t *StreamingTUI) updateStatusBar() {
	t.mu.Lock()
	mode := t.mode
	t.mu.Unlock()

	switch mode {
	case ModeNormal:
		t.statusBar.SetText("[#1e1e2e:#89b4fa] -- NORMAL -- [-:-]  [#6c7086]hjkl:focus  1-9:tab  i:insert  ^U/^D:scroll  ^C:stop  Enter:start  ^R:restart  q:quit[-]")
	case ModeInsert:
		t.statusBar.SetText("[#1e1e2e:#a6e3a1] -- INSERT -- [-:-]  [#6c7086]Esc:normal  (all input forwarded to process)[-]")
	}
}

// updateFocus updates the visual focus indicator for panes in current tab
func (t *StreamingTUI) updateFocus() {
	panes := t.currentTabPanes()
	for i, tv := range panes {
		if i == t.focusIndex {
			tv.SetBorderColor(colLavender)
			tv.SetTitleColor(colLavender)
			t.app.SetFocus(tv)
		} else {
			tv.SetBorderColor(colOverlay0)
			tv.SetTitleColor(colSubtext0)
		}
	}
}

// formatPaneTitle creates a title with status indicator using Catppuccin colors
func formatPaneTitle(name string, running bool, status string) string {
	var indicator string
	var color string

	if !running {
		indicator = "x"
		color = "#f38ba8" // red
	} else {
		switch status {
		case "Healthy":
			indicator = "o"
			color = "#a6e3a1" // green
		case "Checking":
			indicator = "~"
			color = "#f9e2af" // yellow
		case "Unhealthy":
			indicator = "!"
			color = "#fab387" // peach
		default:
			indicator = "?"
			color = "#6c7086" // overlay0
		}
	}

	return fmt.Sprintf(" [%s]%s[-] %s ", color, indicator, name)
}
