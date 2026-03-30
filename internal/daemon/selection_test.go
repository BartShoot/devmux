package daemon

import (
	"fmt"
	"sync"
	"testing"

	"devmux/internal/terminal"
)

func TestSelection_BasicFlow(t *testing.T) {
	sel := &Selection{}

	// Start selection
	sel.HandleMousePress(5, 10)

	startX, startY, endX, endY, active := sel.GetBounds()
	if !active {
		t.Error("Selection should be active after press")
	}
	if startX != 5 || startY != 10 {
		t.Errorf("Wrong start position: got (%d, %d), want (5, 10)", startX, startY)
	}

	// Drag
	sel.HandleMouseDrag(20, 12)
	startX, startY, endX, endY, active = sel.GetBounds()
	if endX != 20 || endY != 12 {
		t.Errorf("Wrong end position after drag: got (%d, %d), want (20, 12)", endX, endY)
	}

	// Release
	sel.HandleMouseRelease(25, 12)
	startX, startY, endX, endY, active = sel.GetBounds()
	if !active {
		t.Error("Selection should remain active after release")
	}
	if endX != 25 || endY != 12 {
		t.Errorf("Wrong end position after release: got (%d, %d), want (25, 12)", endX, endY)
	}

	// Clear
	sel.Clear()
	_, _, _, _, active = sel.GetBounds()
	if active {
		t.Error("Selection should be inactive after clear")
	}
}

func TestSelection_ReverseSelection(t *testing.T) {
	sel := &Selection{}

	// Select from bottom-right to top-left
	sel.HandleMousePress(20, 15)
	sel.HandleMouseRelease(5, 10)

	startX, startY, endX, endY, active := sel.GetBounds()
	if !active {
		t.Error("Selection should be active")
	}

	// Bounds should be normalized (start <= end)
	if startY > endY || (startY == endY && startX > endX) {
		t.Errorf("Selection not normalized: start (%d, %d), end (%d, %d)", startX, startY, endX, endY)
	}

	// Check normalized values
	if startX != 5 || startY != 10 || endX != 20 || endY != 15 {
		t.Errorf("Wrong normalized bounds: start (%d, %d), end (%d, %d)", startX, startY, endX, endY)
	}
}

func TestSelection_GetSelectedText(t *testing.T) {
	term, err := terminal.New(80, 24)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	// Write some text to terminal
	term.Write([]byte("Hello World!\nSecond line here\nThird line"))

	sel := &Selection{}
	sel.HandleMousePress(0, 0)
	sel.HandleMouseRelease(4, 0) // Select "Hello"

	text := sel.GetSelectedText(term)
	if text != "Hello" {
		t.Errorf("Wrong selected text: got %q, want %q", text, "Hello")
	}
}

func TestSelection_ConcurrentUpdates(t *testing.T) {
	term, err := terminal.New(80, 24)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	sel := &Selection{}

	// Start selection
	sel.HandleMousePress(5, 10)

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent writes to terminal
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			if err := term.Write([]byte(fmt.Sprintf("Line %d output text here\n", i))); err != nil {
				errors <- err
				return
			}
		}
	}()

	// Concurrent drags while writes happen
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			sel.HandleMouseDrag(10+i%20, 12)
		}
	}()

	// Concurrent text extraction
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = sel.GetSelectedText(term)
		}
	}()

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}

	// Final release should work
	sel.HandleMouseRelease(30, 12)
	text := sel.GetSelectedText(term)

	// Should not panic and should return some text
	t.Logf("Final selected text length: %d", len(text))
}

func TestSelectionManager(t *testing.T) {
	sm := NewSelectionManager()

	// Get selection for pane 1
	sel1 := sm.GetSelection(1)
	sel1.HandleMousePress(0, 0)
	sel1.HandleMouseRelease(10, 0)

	// Get selection for pane 2
	sel2 := sm.GetSelection(2)
	sel2.HandleMousePress(5, 5)
	sel2.HandleMouseRelease(15, 5)

	// Verify they are independent
	_, _, endX1, _, _ := sel1.GetBounds()
	_, _, endX2, _, _ := sel2.GetBounds()

	if endX1 != 10 {
		t.Errorf("Pane 1 selection corrupted: got endX %d, want 10", endX1)
	}
	if endX2 != 15 {
		t.Errorf("Pane 2 selection corrupted: got endX %d, want 15", endX2)
	}

	// Same pane should return same selection
	sel1Again := sm.GetSelection(1)
	if sel1Again != sel1 {
		t.Error("GetSelection should return same instance for same pane")
	}
}
