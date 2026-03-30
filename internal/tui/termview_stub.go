//go:build !cgo || !ghostty

package tui

import (
	"github.com/rivo/tview"
)

// TerminalView is a stub that wraps TextView when libghostty is not available
type TerminalView struct {
	*tview.TextView
	name       string
	autoScroll bool
}

// NewTerminalView creates a new terminal view (stub using TextView)
func NewTerminalView(name string) *TerminalView {
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetTextAlign(tview.AlignLeft)
	tv.SetBorder(true).SetTitle(" " + name + " ")

	return &TerminalView{
		TextView:   tv,
		name:       name,
		autoScroll: true,
	}
}

// Write appends data to the text view (stub implementation)
func (tv *TerminalView) Write(data []byte) error {
	// Convert ANSI to tview tags and write
	converted := convertANSIToTview(string(data))
	tv.TextView.Write([]byte(converted))
	if tv.autoScroll {
		tv.TextView.ScrollToEnd()
	}
	return nil
}

// SetAutoScroll sets whether to auto-scroll on new content
func (tv *TerminalView) SetAutoScroll(auto bool) {
	tv.autoScroll = auto
}

// GetAutoScroll returns whether auto-scroll is enabled
func (tv *TerminalView) GetAutoScroll() bool {
	return tv.autoScroll
}
