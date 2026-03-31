package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Binary protocol magic bytes - sent by client as first 4 bytes to identify binary protocol
var BinaryMagic = [4]byte{'D', 'M', 'X', 0x01}

// Wire frame: [4-byte payload length (big-endian)][1-byte msg type][payload]

// Cell encoding opcodes
const (
	cellOpSkip = 0x00 // followed by uint8 count: skip N empty cells (1-255)
	cellOpCell = 0x01 // followed by 11-byte cell data
)

// Packed flags byte layout for cells:
// bits 0-3: attrs (bold, italic, underline, strikethrough)
// bit 4: FG is default
// bit 5: BG is default
const (
	packFGDefault = 1 << 4
	packBGDefault = 1 << 5
)

// BinaryWriter writes framed binary messages to a connection.
// Not safe for concurrent use - caller must synchronize.
type BinaryWriter struct {
	w   io.Writer
	buf []byte // reusable scratch buffer
}

func NewBinaryWriter(w io.Writer) *BinaryWriter {
	return &BinaryWriter{
		w:   w,
		buf: make([]byte, 4096),
	}
}

// WriteServerMessage encodes and writes a framed ServerMessage.
func (bw *BinaryWriter) WriteServerMessage(msg *ServerMessage) error {
	bw.buf = bw.buf[:0]

	// Reserve 4 bytes for length prefix (filled in at the end)
	bw.buf = append(bw.buf, 0, 0, 0, 0)

	// Message type
	bw.buf = append(bw.buf, byte(msg.Type))

	// Payload
	switch msg.Type {
	case MsgLayout:
		bw.encodeLayout(msg.Layout)
	case MsgScreenUpdate:
		bw.encodeScreenUpdate(msg.ScreenUpdate)
	case MsgSelection:
		bw.encodeSelection(msg.Selection)
	case MsgPaneStatus:
		bw.encodePaneStatus(msg.PaneStatus)
	case MsgError:
		bw.encodeError(msg.Error)
	default:
		return fmt.Errorf("unknown server message type: %d", msg.Type)
	}

	// Fill in length prefix (total bytes after the 4-byte length field)
	payloadLen := len(bw.buf) - 4
	binary.BigEndian.PutUint32(bw.buf[:4], uint32(payloadLen))

	_, err := bw.w.Write(bw.buf)
	return err
}

// WriteClientMessage encodes and writes a framed ClientMessage.
func (bw *BinaryWriter) WriteClientMessage(msg *ClientMessage) error {
	bw.buf = bw.buf[:0]

	// Reserve 4 bytes for length prefix
	bw.buf = append(bw.buf, 0, 0, 0, 0)

	// Message type
	bw.buf = append(bw.buf, byte(msg.Type))

	// Payload
	switch msg.Type {
	case MsgGetLayout:
		// no payload
	case MsgSubscribe:
		bw.encodeSubscribe(msg.Subscribe)
	case MsgUnsubscribe:
		bw.encodeSubscribe(msg.Subscribe) // same format
	case MsgInput:
		bw.encodeInput(msg.Input)
	case MsgMouse:
		bw.encodeMouse(msg.Mouse)
	case MsgResize:
		bw.encodeResize(msg.Resize)
	case MsgScroll:
		bw.encodeScroll(msg.Scroll)
	case MsgProcessControl:
		bw.encodeProcessControl(msg.ProcessControl)
	default:
		return fmt.Errorf("unknown client message type: %d", msg.Type)
	}

	payloadLen := len(bw.buf) - 4
	binary.BigEndian.PutUint32(bw.buf[:4], uint32(payloadLen))

	_, err := bw.w.Write(bw.buf)
	return err
}

// BinaryReader reads framed binary messages from a connection.
// Not safe for concurrent use - caller must synchronize.
type BinaryReader struct {
	r      io.Reader
	header [4]byte
	buf    []byte // reusable read buffer
}

func NewBinaryReader(r io.Reader) *BinaryReader {
	return &BinaryReader{
		r:   r,
		buf: make([]byte, 4096),
	}
}

// ReadServerMessage reads and decodes one framed ServerMessage.
func (br *BinaryReader) ReadServerMessage() (*ServerMessage, error) {
	return br.ReadServerMessageReuse(nil)
}

// ReadServerMessageReuse reads a ServerMessage, reusing per-pane ScreenUpdate buffers
// from the provided map to avoid allocation on the hot path.
// If reuse is nil, behaves like ReadServerMessage.
func (br *BinaryReader) ReadServerMessageReuse(reuse map[PaneID]*ScreenUpdate) (*ServerMessage, error) {
	payload, err := br.readFrame()
	if err != nil {
		return nil, err
	}
	if len(payload) < 1 {
		return nil, fmt.Errorf("empty message frame")
	}

	msg := &ServerMessage{Type: ServerMsgType(payload[0])}
	data := payload[1:]

	switch msg.Type {
	case MsgLayout:
		msg.Layout, err = decodeLayout(data)
	case MsgScreenUpdate:
		// Peek at pane ID to find reuse buffer (first 4 bytes of data)
		var reuseBuf *ScreenUpdate
		if reuse != nil && len(data) >= 4 {
			paneID := PaneID(binary.BigEndian.Uint32(data[0:4]))
			reuseBuf = reuse[paneID]
		}
		msg.ScreenUpdate, err = DecodeScreenUpdateInto(data, reuseBuf)
		if err == nil && reuse != nil {
			reuse[msg.ScreenUpdate.PaneID] = msg.ScreenUpdate
		}
	case MsgSelection:
		msg.Selection, err = decodeSelection(data)
	case MsgPaneStatus:
		msg.PaneStatus, err = decodePaneStatus(data)
	case MsgError:
		msg.Error, err = decodeError(data)
	default:
		return nil, fmt.Errorf("unknown server message type: %d", msg.Type)
	}

	return msg, err
}

// ReadClientMessage reads and decodes one framed ClientMessage.
func (br *BinaryReader) ReadClientMessage() (*ClientMessage, error) {
	payload, err := br.readFrame()
	if err != nil {
		return nil, err
	}
	if len(payload) < 1 {
		return nil, fmt.Errorf("empty message frame")
	}

	msg := &ClientMessage{Type: ClientMsgType(payload[0])}
	data := payload[1:]

	switch msg.Type {
	case MsgGetLayout:
		// no payload
	case MsgSubscribe:
		msg.Subscribe, err = decodeSubscribe(data)
	case MsgUnsubscribe:
		msg.Subscribe, err = decodeSubscribe(data)
	case MsgInput:
		msg.Input, err = decodeInput(data)
	case MsgMouse:
		msg.Mouse, err = decodeMouse(data)
	case MsgResize:
		msg.Resize, err = decodeResize(data)
	case MsgScroll:
		msg.Scroll, err = decodeScroll(data)
	case MsgProcessControl:
		msg.ProcessControl, err = decodeProcessControl(data)
	default:
		return nil, fmt.Errorf("unknown client message type: %d", msg.Type)
	}

	return msg, err
}

// readFrame reads one length-prefixed frame and returns the payload.
func (br *BinaryReader) readFrame() ([]byte, error) {
	if _, err := io.ReadFull(br.r, br.header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(br.header[:])
	if length > 16*1024*1024 { // 16MB sanity limit
		return nil, fmt.Errorf("frame too large: %d bytes", length)
	}

	// Reuse buffer if large enough
	if int(length) > cap(br.buf) {
		br.buf = make([]byte, length)
	}
	br.buf = br.buf[:length]

	if _, err := io.ReadFull(br.r, br.buf); err != nil {
		return nil, err
	}
	return br.buf, nil
}

// ---------- Encoding helpers ----------

func (bw *BinaryWriter) appendU8(v uint8)   { bw.buf = append(bw.buf, v) }
func (bw *BinaryWriter) appendU16(v uint16) { bw.buf = binary.BigEndian.AppendUint16(bw.buf, v) }
func (bw *BinaryWriter) appendU32(v uint32) { bw.buf = binary.BigEndian.AppendUint32(bw.buf, v) }
func (bw *BinaryWriter) appendU64(v uint64) { bw.buf = binary.BigEndian.AppendUint64(bw.buf, v) }

func (bw *BinaryWriter) appendString(s string) {
	if len(s) > math.MaxUint16 {
		s = s[:math.MaxUint16]
	}
	bw.appendU16(uint16(len(s)))
	bw.buf = append(bw.buf, s...)
}

func (bw *BinaryWriter) appendBool(v bool) {
	if v {
		bw.buf = append(bw.buf, 1)
	} else {
		bw.buf = append(bw.buf, 0)
	}
}

// ---------- ScreenUpdate encoding (the hot path) ----------

func isEmptyCell(c *CellData) bool {
	return (c.Char == 0 || c.Char == ' ') &&
		c.Attrs == 0 &&
		(c.FG.Default || (c.FG.R == 0 && c.FG.G == 0 && c.FG.B == 0)) &&
		(c.BG.Default || (c.BG.R == 0 && c.BG.G == 0 && c.BG.B == 0))
}

func (bw *BinaryWriter) encodeScreenUpdate(u *ScreenUpdate) {
	if u == nil {
		return
	}
	bw.appendU32(uint32(u.PaneID))
	bw.appendU64(u.Sequence)
	var flags uint8
	if u.Full {
		flags |= 1
	}
	if u.Scroll != nil {
		flags |= 2
	}
	bw.appendU8(flags)
	bw.appendU16(u.Cols)
	bw.appendU16(u.Rows)
	bw.appendU16(u.Cursor.X)
	bw.appendU16(u.Cursor.Y)
	bw.appendBool(u.Cursor.Visible)
	if u.Scroll != nil {
		bw.appendU64(u.Scroll.Total)
		bw.appendU64(u.Scroll.Offset)
		bw.appendU64(u.Scroll.Len)
	}

	// Sparse cell encoding
	i := 0
	total := len(u.Cells)
	for i < total {
		if isEmptyCell(&u.Cells[i]) {
			// Count consecutive empty cells
			skip := 0
			for i < total && isEmptyCell(&u.Cells[i]) && skip < 255 {
				skip++
				i++
			}
			bw.appendU8(cellOpSkip)
			bw.appendU8(uint8(skip))
		} else {
			bw.appendU8(cellOpCell)
			bw.encodeCell(&u.Cells[i])
			i++
		}
	}
}

func (bw *BinaryWriter) encodeCell(c *CellData) {
	bw.appendU32(uint32(c.Char))
	bw.buf = append(bw.buf, c.FG.R, c.FG.G, c.FG.B)
	bw.buf = append(bw.buf, c.BG.R, c.BG.G, c.BG.B)
	packed := c.Attrs & 0x0F
	if c.FG.Default {
		packed |= packFGDefault
	}
	if c.BG.Default {
		packed |= packBGDefault
	}
	bw.appendU8(packed)
}

// decodeScreenUpdate decodes a screen update from binary data.
// If reuse is non-nil and its Cells slice has sufficient capacity, it will be reused.
func decodeScreenUpdate(data []byte) (*ScreenUpdate, error) {
	return DecodeScreenUpdateInto(data, nil)
}

// DecodeScreenUpdateInto decodes a screen update, reusing the provided ScreenUpdate's
// Cells buffer if it has sufficient capacity. This avoids per-frame allocation.
func DecodeScreenUpdateInto(data []byte, reuse *ScreenUpdate) (*ScreenUpdate, error) {
	if len(data) < 22 {
		return nil, fmt.Errorf("screen update too short: %d bytes", len(data))
	}

	var u *ScreenUpdate
	if reuse != nil {
		u = reuse
	} else {
		u = &ScreenUpdate{}
	}

	u.PaneID = PaneID(binary.BigEndian.Uint32(data[0:4]))
	u.Sequence = binary.BigEndian.Uint64(data[4:12])
	flags := data[12]
	u.Full = flags&1 != 0
	hasScroll := flags&2 != 0
	u.Cols = binary.BigEndian.Uint16(data[13:15])
	u.Rows = binary.BigEndian.Uint16(data[15:17])
	u.Cursor.X = binary.BigEndian.Uint16(data[17:19])
	u.Cursor.Y = binary.BigEndian.Uint16(data[19:21])
	u.Cursor.Visible = data[21] != 0

	offset := 22
	if hasScroll {
		if offset+24 > len(data) {
			return nil, fmt.Errorf("truncated scroll info")
		}
		u.Scroll = &ScrollInfo{
			Total:  binary.BigEndian.Uint64(data[offset : offset+8]),
			Offset: binary.BigEndian.Uint64(data[offset+8 : offset+16]),
			Len:    binary.BigEndian.Uint64(data[offset+16 : offset+24]),
		}
		offset += 24
	} else {
		u.Scroll = nil
	}

	// Reuse or allocate cell buffer
	totalCells := int(u.Cols) * int(u.Rows)
	if cap(u.Cells) >= totalCells {
		u.Cells = u.Cells[:totalCells]
	} else {
		u.Cells = make([]CellData, totalCells)
	}

	pos := 0

	for offset < len(data) && pos < totalCells {
		op := data[offset]
		offset++

		switch op {
		case cellOpSkip:
			if offset >= len(data) {
				return nil, fmt.Errorf("truncated skip opcode")
			}
			count := int(data[offset])
			offset++
			for j := 0; j < count && pos < totalCells; j++ {
				c := &u.Cells[pos]
				c.Char = ' '
				c.FG = Color{Default: true}
				c.BG = Color{Default: true}
				c.Attrs = 0
				pos++
			}

		case cellOpCell:
			if offset+11 > len(data) {
				return nil, fmt.Errorf("truncated cell data at offset %d", offset)
			}
			c := &u.Cells[pos]
			c.Char = rune(binary.BigEndian.Uint32(data[offset : offset+4]))
			c.FG.R = data[offset+4]
			c.FG.G = data[offset+5]
			c.FG.B = data[offset+6]
			c.BG.R = data[offset+7]
			c.BG.G = data[offset+8]
			c.BG.B = data[offset+9]
			packed := data[offset+10]
			c.Attrs = packed & 0x0F
			c.FG.Default = packed&packFGDefault != 0
			c.BG.Default = packed&packBGDefault != 0
			offset += 11
			pos++

		default:
			return nil, fmt.Errorf("unknown cell opcode: 0x%02x", op)
		}
	}

	// Fill remaining cells as empty
	for pos < totalCells {
		c := &u.Cells[pos]
		c.Char = ' '
		c.FG = Color{Default: true}
		c.BG = Color{Default: true}
		c.Attrs = 0
		pos++
	}

	return u, nil
}

// ---------- Layout encoding ----------

func (bw *BinaryWriter) encodeLayout(l *LayoutMsg) {
	if l == nil {
		bw.appendU8(0)
		return
	}
	bw.appendU8(uint8(len(l.Tabs)))
	for _, tab := range l.Tabs {
		bw.appendU32(uint32(tab.ID))
		bw.appendString(tab.Name)
		bw.appendString(tab.Layout)
		bw.appendU8(uint8(len(tab.Panes)))
		for _, pane := range tab.Panes {
			bw.appendU32(uint32(pane.ID))
			bw.appendString(pane.Name)
			bw.appendBool(pane.Running)
			bw.appendString(pane.Status)
		}
	}
}

func decodeLayout(data []byte) (*LayoutMsg, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("layout data too short")
	}
	r := &binReader{data: data}
	tabCount := r.readU8()
	l := &LayoutMsg{Tabs: make([]TabInfo, tabCount)}
	for i := range l.Tabs {
		l.Tabs[i].ID = TabID(r.readU32())
		l.Tabs[i].Name = r.readString()
		l.Tabs[i].Layout = r.readString()
		paneCount := r.readU8()
		l.Tabs[i].Panes = make([]PaneInfo, paneCount)
		for j := range l.Tabs[i].Panes {
			l.Tabs[i].Panes[j].ID = PaneID(r.readU32())
			l.Tabs[i].Panes[j].Name = r.readString()
			l.Tabs[i].Panes[j].Running = r.readBool()
			l.Tabs[i].Panes[j].Status = r.readString()
		}
	}
	return l, r.err
}

// ---------- Selection encoding ----------

func (bw *BinaryWriter) encodeSelection(s *SelectionMsg) {
	if s == nil {
		return
	}
	bw.appendU32(uint32(s.PaneID))
	bw.appendBool(s.Active)
	bw.appendU16(s.StartX)
	bw.appendU16(s.StartY)
	bw.appendU16(s.EndX)
	bw.appendU16(s.EndY)
	bw.appendString(s.Text)
}

func decodeSelection(data []byte) (*SelectionMsg, error) {
	r := &binReader{data: data}
	s := &SelectionMsg{
		PaneID: PaneID(r.readU32()),
		Active: r.readBool(),
		StartX: r.readU16(),
		StartY: r.readU16(),
		EndX:   r.readU16(),
		EndY:   r.readU16(),
		Text:   r.readString(),
	}
	return s, r.err
}

// ---------- PaneStatus encoding ----------

func (bw *BinaryWriter) encodePaneStatus(p *PaneStatusMsg) {
	if p == nil {
		return
	}
	bw.appendU32(uint32(p.PaneID))
	bw.appendBool(p.Running)
	bw.appendString(p.Status)
}

func decodePaneStatus(data []byte) (*PaneStatusMsg, error) {
	r := &binReader{data: data}
	p := &PaneStatusMsg{
		PaneID:  PaneID(r.readU32()),
		Running: r.readBool(),
		Status:  r.readString(),
	}
	return p, r.err
}

// ---------- Error encoding ----------

func (bw *BinaryWriter) encodeError(e *ErrorMsg) {
	if e == nil {
		return
	}
	bw.appendU32(uint32(e.Code))
	bw.appendString(e.Message)
}

func decodeError(data []byte) (*ErrorMsg, error) {
	r := &binReader{data: data}
	e := &ErrorMsg{
		Code:    int(r.readU32()),
		Message: r.readString(),
	}
	return e, r.err
}

// ---------- Client message encoding ----------

func (bw *BinaryWriter) encodeSubscribe(s *SubscribeMsg) {
	if s == nil {
		bw.appendU8(0)
		return
	}
	bw.appendU8(uint8(len(s.PaneIDs)))
	for _, id := range s.PaneIDs {
		bw.appendU32(uint32(id))
	}
}

func decodeSubscribe(data []byte) (*SubscribeMsg, error) {
	r := &binReader{data: data}
	count := r.readU8()
	s := &SubscribeMsg{PaneIDs: make([]PaneID, count)}
	for i := range s.PaneIDs {
		s.PaneIDs[i] = PaneID(r.readU32())
	}
	return s, r.err
}

func (bw *BinaryWriter) encodeInput(m *InputMsg) {
	if m == nil {
		return
	}
	bw.appendU32(uint32(m.PaneID))
	bw.appendString(m.Data)
}

func decodeInput(data []byte) (*InputMsg, error) {
	r := &binReader{data: data}
	m := &InputMsg{
		PaneID: PaneID(r.readU32()),
		Data:   r.readString(),
	}
	return m, r.err
}

func (bw *BinaryWriter) encodeMouse(m *MouseMsg) {
	if m == nil {
		return
	}
	bw.appendU32(uint32(m.PaneID))
	bw.appendU8(uint8(m.Action))
	bw.appendU16(m.X)
	bw.appendU16(m.Y)
}

func decodeMouse(data []byte) (*MouseMsg, error) {
	r := &binReader{data: data}
	m := &MouseMsg{
		PaneID: PaneID(r.readU32()),
		Action: MouseAction(r.readU8()),
		X:      r.readU16(),
		Y:      r.readU16(),
	}
	return m, r.err
}

func (bw *BinaryWriter) encodeResize(m *ResizeMsg) {
	if m == nil {
		return
	}
	bw.appendU32(uint32(m.PaneID))
	bw.appendU16(m.Cols)
	bw.appendU16(m.Rows)
}

func decodeResize(data []byte) (*ResizeMsg, error) {
	r := &binReader{data: data}
	m := &ResizeMsg{
		PaneID: PaneID(r.readU32()),
		Cols:   r.readU16(),
		Rows:   r.readU16(),
	}
	return m, r.err
}

// ---------- Scroll encoding ----------

func (bw *BinaryWriter) encodeScroll(m *ScrollMsg) {
	if m == nil {
		return
	}
	bw.appendU32(uint32(m.PaneID))
	bw.appendU8(uint8(m.Action))
	bw.appendU16(uint16(m.Amount))
}

func decodeScroll(data []byte) (*ScrollMsg, error) {
	r := &binReader{data: data}
	m := &ScrollMsg{
		PaneID: PaneID(r.readU32()),
		Action: ScrollAction(r.readU8()),
		Amount: int16(r.readU16()),
	}
	return m, r.err
}

// ---------- ProcessControl encoding ----------

func (bw *BinaryWriter) encodeProcessControl(m *ProcessControlMsg) {
	if m == nil {
		return
	}
	bw.appendU32(uint32(m.PaneID))
	bw.appendU8(uint8(m.Action))
}

func decodeProcessControl(data []byte) (*ProcessControlMsg, error) {
	r := &binReader{data: data}
	m := &ProcessControlMsg{
		PaneID: PaneID(r.readU32()),
		Action: ProcessAction(r.readU8()),
	}
	return m, r.err
}

// ---------- binReader: simple cursor-based reader with error tracking ----------

type binReader struct {
	data   []byte
	offset int
	err    error
}

func (r *binReader) readU8() uint8 {
	if r.err != nil || r.offset+1 > len(r.data) {
		r.err = fmt.Errorf("unexpected end of data at offset %d", r.offset)
		return 0
	}
	v := r.data[r.offset]
	r.offset++
	return v
}

func (r *binReader) readU16() uint16 {
	if r.err != nil || r.offset+2 > len(r.data) {
		r.err = fmt.Errorf("unexpected end of data at offset %d", r.offset)
		return 0
	}
	v := binary.BigEndian.Uint16(r.data[r.offset : r.offset+2])
	r.offset += 2
	return v
}

func (r *binReader) readU32() uint32 {
	if r.err != nil || r.offset+4 > len(r.data) {
		r.err = fmt.Errorf("unexpected end of data at offset %d", r.offset)
		return 0
	}
	v := binary.BigEndian.Uint32(r.data[r.offset : r.offset+4])
	r.offset += 4
	return v
}

func (r *binReader) readBool() bool {
	return r.readU8() != 0
}

func (r *binReader) readString() string {
	length := r.readU16()
	if r.err != nil || r.offset+int(length) > len(r.data) {
		r.err = fmt.Errorf("unexpected end of data at offset %d (string len %d)", r.offset, length)
		return ""
	}
	s := string(r.data[r.offset : r.offset+int(length)])
	r.offset += int(length)
	return s
}
