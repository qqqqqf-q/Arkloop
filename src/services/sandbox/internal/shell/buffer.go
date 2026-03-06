package shell

const (
	RingBufferBytes = 1 << 20
	ReadChunkBytes  = 64 * 1024

	TailBufferBytes     = 32 * 1024
	TranscriptHeadBytes = 64 * 1024
	TranscriptTailBytes = 128 * 1024
)

type RingBuffer struct {
	maxSize     int
	startCursor uint64
	endCursor   uint64
	buf         []byte
}

func NewRingBuffer(maxSize int) *RingBuffer {
	if maxSize <= 0 {
		maxSize = RingBufferBytes
	}
	return &RingBuffer{
		maxSize: maxSize,
		buf:     make([]byte, 0, minInt(maxSize, 4096)),
	}
}

func (b *RingBuffer) Append(data []byte) {
	if len(data) == 0 {
		return
	}
	if len(data) >= b.maxSize {
		trimmed := data[len(data)-b.maxSize:]
		b.startCursor = b.endCursor + uint64(len(data)-b.maxSize)
		b.endCursor += uint64(len(data))
		b.buf = append(b.buf[:0], trimmed...)
		return
	}

	b.buf = append(b.buf, data...)
	b.endCursor += uint64(len(data))

	over := len(b.buf) - b.maxSize
	if over <= 0 {
		return
	}
	b.buf = append([]byte(nil), b.buf[over:]...)
	b.startCursor += uint64(over)
}

func (b *RingBuffer) ReadFrom(cursor uint64, limit int) ([]byte, uint64, bool, bool) {
	if limit <= 0 {
		limit = ReadChunkBytes
	}
	if cursor > b.endCursor {
		return nil, 0, false, false
	}

	truncated := false
	if cursor < b.startCursor {
		cursor = b.startCursor
		truncated = true
	}
	if cursor == b.endCursor {
		return nil, b.endCursor, truncated, true
	}

	offset := int(cursor - b.startCursor)
	available := len(b.buf) - offset
	if available < 0 {
		available = 0
	}
	if available > limit {
		available = limit
	}
	if available == 0 {
		return nil, cursor, truncated, true
	}

	chunk := append([]byte(nil), b.buf[offset:offset+available]...)
	return chunk, cursor + uint64(available), truncated, true
}

func (b *RingBuffer) EndCursor() uint64 {
	return b.endCursor
}

func (b *RingBuffer) StartCursor() uint64 {
	return b.startCursor
}

func (b *RingBuffer) Len() int {
	return len(b.buf)
}

func (b *RingBuffer) Bytes() []byte {
	return append([]byte(nil), b.buf...)
}

type HeadTailBuffer struct {
	headLimit int
	tailLimit int
	head      []byte
	tail      *RingBuffer
	totalSize int64
}

type HeadTailSnapshot struct {
	Text         string
	Truncated    bool
	OmittedBytes int64
}

func NewHeadTailBuffer(headLimit, tailLimit int) *HeadTailBuffer {
	if headLimit < 0 {
		headLimit = 0
	}
	if tailLimit < 0 {
		tailLimit = 0
	}
	return &HeadTailBuffer{
		headLimit: headLimit,
		tailLimit: tailLimit,
		head:      make([]byte, 0, minInt(headLimit, 4096)),
		tail:      NewRingBuffer(tailLimit),
	}
}

func (b *HeadTailBuffer) Append(data []byte) {
	if len(data) == 0 {
		return
	}
	b.totalSize += int64(len(data))
	if b.headLimit > len(b.head) {
		remaining := b.headLimit - len(b.head)
		copied := minInt(remaining, len(data))
		b.head = append(b.head, data[:copied]...)
		data = data[copied:]
	}
	if len(data) == 0 || b.tailLimit == 0 {
		return
	}
	b.tail.Append(data)
}

func (b *HeadTailBuffer) Snapshot() HeadTailSnapshot {
	tail := b.tail.Bytes()
	stored := int64(len(b.head) + len(tail))
	omitted := b.totalSize - stored
	if omitted < 0 {
		omitted = 0
	}
	text := string(b.head) + string(tail)
	return HeadTailSnapshot{
		Text:         text,
		Truncated:    omitted > 0,
		OmittedBytes: omitted,
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
