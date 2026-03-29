package daemon

import (
	"bytes"
	"sync"
)

type LogBuffer struct {
	lines    [][]byte
	maxLines int
	start    int // Index of the oldest line
	size     int // Current number of lines stored
	current  bytes.Buffer
	mu       sync.RWMutex
}

func NewLogBuffer(maxLines int) *LogBuffer {
	return &LogBuffer{
		lines:    make([][]byte, maxLines),
		maxLines: maxLines,
	}
}

func (lb *LogBuffer) Write(p []byte) (n int, err error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, b := range p {
		if b == '\n' {
			lb.addLine(lb.current.Bytes())
			lb.current.Reset()
		} else if b != '\r' {
			lb.current.WriteByte(b)
		}
	}

	return len(p), nil
}

// addLine adds a line to the ring buffer, overwriting the oldest if full
func (lb *LogBuffer) addLine(line []byte) {
	// Copy the data to a new slice to avoid referencing the current buffer's backing array
	newLine := make([]byte, len(line))
	copy(newLine, line)

	if lb.size < lb.maxLines {
		lb.lines[lb.size] = newLine
		lb.size++
	} else {
		lb.lines[lb.start] = newLine
		lb.start = (lb.start + 1) % lb.maxLines
	}
}

func (lb *LogBuffer) GetLines() []string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	result := make([]string, 0, lb.size+1)
	for i := 0; i < lb.size; i++ {
		idx := (lb.start + i) % lb.maxLines
		result = append(result, string(lb.lines[idx]))
	}

	// Include the current (incomplete) line if it has data
	if lb.current.Len() > 0 {
		result = append(result, lb.current.String())
	}
	return result
}

func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.start = 0
	lb.size = 0
	lb.current.Reset()
	// Clear the slices to help GC
	for i := range lb.lines {
		lb.lines[i] = nil
	}
}
