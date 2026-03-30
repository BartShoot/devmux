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

	// Raw byte buffer for terminal emulation
	rawBuf     []byte
	rawMaxSize int
}

func NewLogBuffer(maxLines int) *LogBuffer {
	return &LogBuffer{
		lines:      make([][]byte, maxLines),
		maxLines:   maxLines,
		rawMaxSize: 1024 * 1024, // 1MB raw buffer
	}
}

func (lb *LogBuffer) Write(p []byte) (n int, err error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// Store raw bytes for terminal emulation
	lb.rawBuf = append(lb.rawBuf, p...)
	// Trim if exceeds max size (keep the newest data)
	if len(lb.rawBuf) > lb.rawMaxSize {
		lb.rawBuf = lb.rawBuf[len(lb.rawBuf)-lb.rawMaxSize:]
	}

	// Also store line-based for backward compatibility
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
	lb.rawBuf = nil
	// Clear the slices to help GC
	for i := range lb.lines {
		lb.lines[i] = nil
	}
}

// GetRaw returns raw bytes starting from offset.
// Returns the data, new offset (total bytes seen), and whether the buffer was truncated.
func (lb *LogBuffer) GetRaw(offset int) (data []byte, totalBytes int, truncated bool) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	totalBytes = len(lb.rawBuf)

	if offset >= totalBytes {
		return nil, totalBytes, false
	}

	if offset < 0 {
		offset = 0
	}

	// Return slice from offset to end
	data = make([]byte, totalBytes-offset)
	copy(data, lb.rawBuf[offset:])

	return data, totalBytes, false
}

// GetRawTotal returns the total number of raw bytes in the buffer
func (lb *LogBuffer) GetRawTotal() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return len(lb.rawBuf)
}
