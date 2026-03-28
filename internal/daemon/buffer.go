package daemon

import (
	"bytes"
	"sync"
)

type LogBuffer struct {
	lines    []string
	maxLines int
	current  bytes.Buffer
	mu       sync.RWMutex
}

func NewLogBuffer(maxLines int) *LogBuffer {
	return &LogBuffer{
		lines:    make([]string, 0, maxLines),
		maxLines: maxLines,
	}
}

func (lb *LogBuffer) Write(p []byte) (n int, err error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, b := range p {
		if b == '\n' {
			lb.lines = append(lb.lines, lb.current.String())
			lb.current.Reset()
			if len(lb.lines) > lb.maxLines {
				lb.lines = lb.lines[len(lb.lines)-lb.maxLines:]
			}
		} else if b != '\r' {
			lb.current.WriteByte(b)
		}
	}

	return len(p), nil
}

func (lb *LogBuffer) GetLines() []string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	result := make([]string, len(lb.lines))
	copy(result, lb.lines)
	// Optionally include the current line if it's not empty
	if lb.current.Len() > 0 {
		result = append(result, lb.current.String())
	}
	return result
}
