package sqlite

type Meta struct {
	StartOffset int
	Size        int
}

type Uint8Field struct {
	Meta  Meta
	Value uint8
}

type Uint16Field struct {
	Meta  Meta
	Value uint16
}

type Uint32Field struct {
	Meta  Meta
	Value uint32
}

type Uint64Field struct {
	Meta  Meta
	Value uint64
}

type VarintField struct {
	Meta  Meta
	Value uint64
}

type PageKindField struct {
	Meta  Meta
	Value PageKindType
}

func (m Meta) Valid() bool {
	return m.Size >= 0 && m.StartOffset >= 0
}

func (m Meta) EndOffset() int {
	return m.StartOffset + m.Size
}

func (m Meta) FileStartOffset(pageNumber uint32, pageSize uint32) int64 {
	return int64(pageNumber-1)*int64(pageSize) + int64(m.StartOffset)
}

func (m Meta) FileEndOffset(pageNumber uint32, pageSize uint32) int64 {
	return int64(pageNumber-1)*int64(pageSize) + int64(m.EndOffset())
}

func metaFromPage(pageNumber uint32, pageSize uint32, start int, size int) Meta {
	_, _ = pageNumber, pageSize
	return Meta{StartOffset: start, Size: size}
}
