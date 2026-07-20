package storage

type Database interface {
	Close() error
	Engine() Engine

	Overview() (*DatabaseOverview, error)
	InspectPage(PageRef) (*PageInspection, error)
	PagesForBTree(BTreeID) ([]PageRef, error)
}

type Engine string

const (
	EngineSQLite Engine = "sqlite"
	EngineBbolt  Engine = "bbolt"
)

type DatabaseOverview struct {
	Path              string
	PageSizeBytes     uint64
	PageCount         uint64
	FirstPageID       uint64
	DatabaseSizeBytes uint64

	HeaderRows []Field
	BTrees     []BTreeItem
}

type BTreeID string

type BTreeItem struct {
	ID       BTreeID
	Kind     BTreeKind
	Name     string
	RootPage *PageRef
	System   bool
	Rows     []Field
}

type BTreeKind string

const (
	BTreeTable        BTreeKind = "table"
	BTreeIndex        BTreeKind = "index"
	BTreeBucket       BTreeKind = "bucket"
	BTreeInlineBucket BTreeKind = "inline_bucket"
	BTreeRootless     BTreeKind = "rootless"
)

type PageRef struct {
	ID uint64
}

type PageInspection struct {
	Ref       PageRef
	Raw       []byte
	Rows      []Field
	HexBlocks []HexBlock
}

type HexBlock struct {
	ID       string
	Kind     string
	Title    string
	Span     ByteSpan
	Rows     []Field
	Children []HexBlock
}

type ByteSpan struct {
	Start int
	Size  int
}

func (s ByteSpan) Valid() bool {
	return s.Start >= 0 && s.Size >= 0
}

func (s ByteSpan) End() int {
	return s.Start + s.Size
}

type Field struct {
	Label string
	Value string
	Span  *ByteSpan
}

func Section(label string) Field {
	return Field{Label: "$section", Value: label}
}

func Blank() Field {
	return Field{}
}
