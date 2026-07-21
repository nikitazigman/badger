package bbolt

type BTreePage struct {
	ID        PageID
	Raw       []byte
	Header    PageHeader
	HasHeader bool

	Classification PageClassification
	ContinuationOf *PageID
	OverflowExtent *OverflowExtent

	MetaPayload *MetaPayload

	BranchPayload *BranchPayload

	LeafPayload *LeafPayload

	FreelistPayload *FreelistPayload
}

type Meta struct {
	StartOffset int
	Size        int
}

func (m Meta) Valid() bool {
	return m.StartOffset >= 0 && m.Size >= 0
}

func (m Meta) EndOffset() int {
	return m.StartOffset + m.Size
}

func metaFromOffset(start int, size int) Meta {
	return Meta{StartOffset: start, Size: size}
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

type PageIDField struct {
	Meta  Meta
	Value PageID
}

type FlagField struct {
	Meta  Meta
	Value FlagType
}

type FlagType uint16
type PageID uint64

const (
	BranchPageFlag   FlagType = 0x01
	LeafPageFlag     FlagType = 0x02
	MetaPageFlag     FlagType = 0x04
	FreelistPageFlag FlagType = 0x10
)

const PgidNoFreelist PageID = 0xffffffffffffffff

type PageClassification string

const (
	PageClassMeta         PageClassification = "meta"
	PageClassBranch       PageClassification = "branch"
	PageClassLeaf         PageClassification = "leaf"
	PageClassFreelist     PageClassification = "freelist"
	PageClassFree         PageClassification = "free"
	PageClassContinuation PageClassification = "continuation"
	PageClassUnknown      PageClassification = "unknown"
	PageClassTruncated    PageClassification = "truncated"
)

type PageSummary struct {
	ID             PageID
	Classification PageClassification
	OverflowExtent *OverflowExtent
}

type OverflowExtent struct {
	Parent    PageID
	Page      PageID
	Start     PageID
	End       PageID
	PartIndex uint32
	PartCount uint32
	Span      Meta
}

type PageHeader struct {
	Meta     Meta
	ID       PageIDField
	Flags    FlagField
	Count    Uint16Field
	Overflow Uint32Field
}

const (
	Magic   = 0xED0CDAED
	Version = 0x02
)

type MetaPayload struct {
	Meta          Meta
	Magic         Uint32Field
	Version       Uint32Field
	PageSize      Uint32Field
	Flags         Uint32Field
	Root          PageIDField
	Sequence      Uint64Field
	FreeList      PageIDField
	PageID        PageIDField
	TransactionID Uint64Field
	CheckSum      Uint64Field // FNV-1 checksum
}

type BranchPayload struct {
	Meta           Meta
	BranchElements []BranchElement
	KeyValue       []KeyValue
}

type BranchElement struct {
	Meta    Meta
	Pos     Uint32Field
	KeySize Uint32Field
	PageID  PageIDField
}

type KeyValue struct {
	Meta  Meta
	Key   Payload
	Value Payload
}

type Payload struct {
	Meta Meta
	Data []byte
}

// TODO: how to represent inline page
type LeafPayload struct {
	Meta         Meta
	LeafElements []LeafElement
	KeyValue     []KeyValue
	NestedBucket []NestedBucket
	InlineBucket []InlineBucket
}

type LeafFlagType uint32

const (
	OrdinaryKeyValueFlag LeafFlagType = 0x00
	BucketLeafFlag       LeafFlagType = 0x01
	NestedBucketFlag                  = BucketLeafFlag
)

type LeafElement struct {
	Meta      Meta
	Flags     LeafFlagField
	Pos       Uint32Field
	KeySize   Uint32Field
	ValueSize Uint32Field
}

type LeafFlagField struct {
	Meta  Meta
	Value LeafFlagType
}

type NestedBucket struct {
	Meta     Meta
	Root     PageIDField // 0 for inline bucket
	Sequence Uint64Field
}

type BucketEntry struct {
	PageID       PageID
	ElementIndex int
	Key          Payload
	Bucket       NestedBucket
	Inline       *InlineBucket
}

type InlineBucket struct {
	Meta        Meta
	Header      PageHeader
	LeafPayload LeafPayload
}

type FreelistPayload struct {
	Meta        Meta
	ActualCount *Uint64Field
	IDFields    []PageIDField
	IDs         []PageID
}
