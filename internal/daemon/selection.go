package daemon

import (
	"sync"

	"devmux/internal/protocol"
	"devmux/internal/terminal"
)

// Selection represents a text selection state
type Selection struct {
	Active  bool
	StartX  int
	StartY  int
	EndX    int
	EndY    int
	mu      sync.RWMutex
}

// SelectionManager manages selections across all panes
type SelectionManager struct {
	selections map[protocol.PaneID]*Selection
	mu         sync.RWMutex
}

func NewSelectionManager() *SelectionManager {
	return &SelectionManager{
		selections: make(map[protocol.PaneID]*Selection),
	}
}

// GetSelection returns the selection for a pane, creating one if needed
func (sm *SelectionManager) GetSelection(paneID protocol.PaneID) *Selection {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sel, ok := sm.selections[paneID]; ok {
		return sel
	}

	sel := &Selection{}
	sm.selections[paneID] = sel
	return sel
}

// HandleMousePress starts a new selection
func (s *Selection) HandleMousePress(x, y int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Active = true
	s.StartX = x
	s.StartY = y
	s.EndX = x
	s.EndY = y
}

// HandleMouseDrag extends the selection
func (s *Selection) HandleMouseDrag(x, y int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.Active {
		return
	}

	s.EndX = x
	s.EndY = y
}

// HandleMouseRelease completes the selection
func (s *Selection) HandleMouseRelease(x, y int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.EndX = x
	s.EndY = y
	// Keep selection active until cleared explicitly
}

// Clear clears the selection
func (s *Selection) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Active = false
	s.StartX = 0
	s.StartY = 0
	s.EndX = 0
	s.EndY = 0
}

// GetBounds returns the selection bounds in normalized form (start <= end)
func (s *Selection) GetBounds() (startX, startY, endX, endY int, active bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.Active {
		return 0, 0, 0, 0, false
	}

	// Normalize so start comes before end
	sy, ey := s.StartY, s.EndY
	sx, ex := s.StartX, s.EndX

	if sy > ey || (sy == ey && sx > ex) {
		// Swap start and end
		sy, ey = ey, sy
		sx, ex = ex, sx
	}

	return sx, sy, ex, ey, true
}

// GetSelectedText extracts selected text from terminal
func (s *Selection) GetSelectedText(term *terminal.Terminal) string {
	if term == nil {
		return ""
	}

	startX, startY, endX, endY, active := s.GetBounds()
	if !active {
		return ""
	}

	screen := term.GetScreen()
	if len(screen) == 0 {
		return ""
	}

	var result []rune

	for y := startY; y <= endY && y < len(screen); y++ {
		row := screen[y]
		var lineStart, lineEnd int

		if y == startY {
			lineStart = startX
		} else {
			lineStart = 0
		}

		if y == endY {
			lineEnd = endX + 1
		} else {
			lineEnd = len(row)
		}

		// Clamp to row bounds
		if lineStart >= len(row) {
			lineStart = len(row) - 1
		}
		if lineStart < 0 {
			lineStart = 0
		}
		if lineEnd > len(row) {
			lineEnd = len(row)
		}

		// Extract characters
		for x := lineStart; x < lineEnd; x++ {
			ch := row[x].Char
			if ch == 0 {
				ch = ' '
			}
			result = append(result, ch)
		}

		// Add newline between rows (except last)
		if y < endY {
			result = append(result, '\n')
		}
	}

	// Trim trailing spaces from each line
	return trimTrailingSpaces(string(result))
}

// trimTrailingSpaces removes trailing spaces from each line
func trimTrailingSpaces(s string) string {
	var result []rune
	var lineSpaces []rune

	for _, ch := range s {
		if ch == ' ' {
			lineSpaces = append(lineSpaces, ch)
		} else if ch == '\n' {
			// Don't include trailing spaces before newline
			result = append(result, '\n')
			lineSpaces = nil
		} else {
			// Include accumulated spaces and this character
			result = append(result, lineSpaces...)
			result = append(result, ch)
			lineSpaces = nil
		}
	}

	// Don't include final trailing spaces
	return string(result)
}

// ToProtocol converts selection state to protocol format
func (s *Selection) ToProtocol(paneID protocol.PaneID, text string) *protocol.SelectionMsg {
	startX, startY, endX, endY, active := s.GetBounds()

	return &protocol.SelectionMsg{
		PaneID: paneID,
		Active: active,
		StartX: uint16(startX),
		StartY: uint16(startY),
		EndX:   uint16(endX),
		EndY:   uint16(endY),
		Text:   text,
	}
}
