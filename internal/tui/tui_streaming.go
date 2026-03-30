package tui

import (
	"fmt"
	"strings"
	"sync"

	"devmux/internal/protocol"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// StreamingTUI is the new TUI using push-based streaming protocol
type StreamingTUI struct {
	app        *tview.Application
	pages      *tview.Pages
	tabBar     *tview.TextView
	client     *StreamClient
	network    string
	addr       string

	// Pane management
	panes      map[protocol.PaneID]*SimpleTerminalView
	paneList   []*SimpleTerminalView
	focusIndex int

	// Tab management
	tabs       []tabState
	currentTab int

	mu sync.Mutex
}

type tabState struct {
	id     protocol.TabID
	name   string
	panes  []protocol.PaneID
}

// NewStreamingTUI creates a new streaming TUI
func NewStreamingTUI(network, addr string) *StreamingTUI {
	return &StreamingTUI{
		app:     tview.NewApplication(),
		pages:   tview.NewPages(),
		network: network,
		addr:    addr,
		panes:   make(map[protocol.PaneID]*SimpleTerminalView),
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

	// Wait a bit for layout to arrive before setting up UI
	// In production, we'd use a channel/sync mechanism
	// For now, the layout handler will build the UI

	// Create tab bar
	t.tabBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	t.tabBar.SetBackgroundColor(tcell.ColorDarkBlue)

	// Main layout: tab bar on top, content below
	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.tabBar, 1, 0, false).
		AddItem(t.pages, 0, 1, true)

	// Global input handling
	t.app.SetInputCapture(t.handleInput)

	// Handle window resize
	t.app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		// Check if any pane sizes changed and send resize messages
		t.mu.Lock()
		for paneID, tv := range t.panes {
			width, height := tv.GetInnerSize()
			// Store last known size to avoid spamming
			if width > 0 && height > 0 {
				if tv.viewCols != width || tv.viewRows != height {
					tv.viewCols = width
					tv.viewRows = height
					go t.client.SendResize(paneID, width, height)
				}
			}
		}
		t.mu.Unlock()
		return false // Don't skip draw
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
		t.paneList = nil
		t.panes = make(map[protocol.PaneID]*SimpleTerminalView)

		// Build tabs
		for _, tab := range layout.Tabs {
			tabContent := t.buildTabContent(tab)
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
		}

		t.updateTabBar()

		// Set focus to first pane
		if len(t.paneList) > 0 {
			t.focusIndex = 0
			t.updateFocus()
		}

		// Subscribe to all panes
		t.subscribeToAllPanes()
	})
}

// buildTabContent creates the layout for a tab
func (t *StreamingTUI) buildTabContent(tab protocol.TabInfo) tview.Primitive {
	if len(tab.Panes) == 0 {
		return tview.NewBox()
	}

	// Create views for each pane
	var paneViews []*SimpleTerminalView
	for _, pane := range tab.Panes {
		tv := NewSimpleTerminalView(pane.ID, pane.Name)
		// Set initial title with status from layout
		title := formatPaneTitle(pane.Name, pane.Running, pane.Status)
		tv.SetTitle(title)
		paneViews = append(paneViews, tv)
		t.panes[pane.ID] = tv
		t.paneList = append(t.paneList, tv)
	}

	// Apply layout
	layout := strings.ToLower(tab.Layout)
	if layout == "" {
		layout = "vertical"
	}

	switch layout {
	case "horizontal":
		flex := tview.NewFlex().SetDirection(tview.FlexColumn)
		for _, tv := range paneViews {
			flex.AddItem(tv, 0, 1, false)
		}
		return flex

	case "split":
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

// subscribeToAllPanes subscribes to updates for all panes
func (t *StreamingTUI) subscribeToAllPanes() {
	var paneIDs []protocol.PaneID
	for id := range t.panes {
		paneIDs = append(paneIDs, id)
	}
	if len(paneIDs) > 0 {
		t.client.SendSubscribe(paneIDs)
	}

	// Send initial resize for each pane based on actual dimensions
	// This is deferred slightly to allow tview to calculate layout
	go func() {
		// Small delay to let tview compute actual sizes
		// In a real app, you'd hook into tview's resize event
		t.app.QueueUpdateDraw(func() {
			t.sendResizeForAllPanes()
		})
	}()
}

// sendResizeForAllPanes sends resize messages for all panes based on their actual size
func (t *StreamingTUI) sendResizeForAllPanes() {
	for paneID, tv := range t.panes {
		width, height := tv.GetInnerSize()
		if width > 0 && height > 0 {
			t.client.SendResize(paneID, width, height)
		}
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

		// If selection has text, could copy to clipboard here
		// For now, just update the display
	}
}

// handlePaneStatus processes pane status updates
func (t *StreamingTUI) handlePaneStatus(status *protocol.PaneStatusMsg) {
	t.mu.Lock()
	tv, ok := t.panes[status.PaneID]
	t.mu.Unlock()

	if ok {
		// Update title with status
		t.app.QueueUpdateDraw(func() {
			title := formatPaneTitle(tv.name, status.Running, status.Status)
			tv.SetTitle(title)
		})
	}
}

// handleError processes error messages
func (t *StreamingTUI) handleError(err *protocol.ErrorMsg) {
	// Could show error in status bar
	fmt.Printf("Error from daemon: %s\n", err.Message)
}

// handleInput processes keyboard input
func (t *StreamingTUI) handleInput(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Rune() == 'q' || event.Key() == tcell.KeyCtrlC:
		t.app.Stop()
		return nil

	case event.Key() == tcell.KeyTab:
		// Cycle focus within current tab
		t.mu.Lock()
		if len(t.paneList) > 0 {
			t.focusIndex = (t.focusIndex + 1) % len(t.paneList)
			t.updateFocus()
		}
		t.mu.Unlock()
		return nil

	case event.Rune() >= '1' && event.Rune() <= '9':
		// Switch tabs with number keys
		tabIndex := int(event.Rune() - '1')
		t.switchTab(tabIndex)
		return nil

	case event.Key() == tcell.KeyLeft:
		t.mu.Lock()
		if t.currentTab > 0 {
			t.mu.Unlock()
			t.switchTab(t.currentTab - 1)
		} else {
			t.mu.Unlock()
		}
		return nil

	case event.Key() == tcell.KeyRight:
		t.mu.Lock()
		if t.currentTab < len(t.tabs)-1 {
			t.mu.Unlock()
			t.switchTab(t.currentTab + 1)
		} else {
			t.mu.Unlock()
		}
		return nil
	}

	// Forward input to focused pane
	t.mu.Lock()
	if t.focusIndex < len(t.paneList) {
		paneID := t.paneList[t.focusIndex].PaneID()
		t.mu.Unlock()

		var input string
		switch event.Key() {
		case tcell.KeyEnter:
			input = "\n"
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			input = "\x7f"
		case tcell.KeyCtrlD:
			input = "\x04"
		case tcell.KeyEscape:
			input = "\x1b"
		case tcell.KeyRune:
			input = string(event.Rune())
		default:
			return event // Let arrow keys etc pass through
		}

		if input != "" {
			go t.client.SendInput(paneID, input)
			return nil
		}
	} else {
		t.mu.Unlock()
	}

	return event
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
}

// updateTabBar updates the tab bar display
func (t *StreamingTUI) updateTabBar() {
	var parts []string
	for i, tab := range t.tabs {
		if i == t.currentTab {
			parts = append(parts, fmt.Sprintf("[black:white] %d:%s [-:-]", i+1, tab.name))
		} else {
			parts = append(parts, fmt.Sprintf(" %d:%s ", i+1, tab.name))
		}
	}
	t.tabBar.SetText(strings.Join(parts, "│") + "  [gray](←/→ switch tabs, Tab cycle panes, type to input, q quit)[-]")
}

// updateFocus updates the visual focus indicator
func (t *StreamingTUI) updateFocus() {
	for i, tv := range t.paneList {
		if i == t.focusIndex {
			tv.SetBorderColor(tcell.ColorYellow)
			t.app.SetFocus(tv)
		} else {
			tv.SetBorderColor(tcell.ColorWhite)
		}
	}
}

// formatPaneTitle creates a title with status indicator
func formatPaneTitle(name string, running bool, status string) string {
	var indicator string
	var color string

	if !running {
		indicator = "✗"
		color = "red"
	} else {
		switch status {
		case "Healthy":
			indicator = "✓"
			color = "green"
		case "Checking":
			indicator = "◐"
			color = "yellow"
		case "Unhealthy":
			indicator = "!"
			color = "orange"
		default:
			indicator = "?"
			color = "gray"
		}
	}

	return fmt.Sprintf(" [%s]%s[-] %s ", color, indicator, name)
}
