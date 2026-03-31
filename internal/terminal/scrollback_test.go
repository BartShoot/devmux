//go:build cgo && ghostty

package terminal

import (
	"fmt"
	"strings"
	"testing"
)

func TestScrollbackCapacity(t *testing.T) {
	// With 10MB max_scrollback (~10k lines at 80 cols),
	// we should be able to store well over 5000 lines.
	term, err := New(80, 24)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	batches := []int{100, 500, 1000, 2000, 5000}

	totalWritten := 0
	for _, target := range batches {
		linesToWrite := target - totalWritten

		var sb strings.Builder
		for i := 0; i < linesToWrite; i++ {
			fmt.Fprintf(&sb, "Line %05d\n", totalWritten+i+1)
		}
		term.Write([]byte(sb.String()))
		totalWritten = target

		total, offset, length := term.GetScrollbar()
		scrollbackRows := total - length

		t.Logf("After %5d lines written: total=%d, offset=%d, viewport=%d, scrollback=%d",
			totalWritten, total, offset, length, scrollbackRows)

		expectedScrollback := totalWritten - 24
		if expectedScrollback < 0 {
			expectedScrollback = 0
		}

		if int(scrollbackRows) < expectedScrollback/2 {
			t.Errorf("Scrollback too small: got %d, expected ~%d (wrote %d lines, viewport %d)",
				scrollbackRows, expectedScrollback, totalWritten, length)
		}
	}
}

func TestScrollViewportAfterOutput(t *testing.T) {
	term, err := New(80, 24)
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}
	defer term.Close()

	var sb strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&sb, "Line %03d\n", i+1)
	}
	term.Write([]byte(sb.String()))

	total, offset, length := term.GetScrollbar()
	t.Logf("Before scroll: total=%d, offset=%d, viewport=%d", total, offset, length)

	term.ScrollViewport(1, 100) // up
	total2, offset2, length2 := term.GetScrollbar()
	t.Logf("After scroll up 100: total=%d, offset=%d, viewport=%d", total2, offset2, length2)

	if offset2 >= offset {
		t.Errorf("Scroll up should decrease offset: before=%d, after=%d", offset, offset2)
	}

	term.ScrollViewport(4, 0) // bottom
	total3, offset3, length3 := term.GetScrollbar()
	t.Logf("After scroll bottom: total=%d, offset=%d, viewport=%d", total3, offset3, length3)

	if offset3 != offset {
		t.Errorf("Scroll bottom should restore offset: expected=%d, got=%d", offset, offset3)
	}
	_ = length
	_ = length2
	_ = length3
}
