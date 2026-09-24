package tiff

type ByteOrder string

const (
	BigEndian    ByteOrder = "big-endian"
	LittleEndian ByteOrder = "little-endian"
)

type ByteRange struct {
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
	End    int64 `json:"end"`
}

type RawValue struct {
	Type   string   `json:"type"`
	Count  int      `json:"count"`
	Values []string `json:"values"`
	Hex    string   `json:"hex"`
	Inline bool     `json:"inline"`
}

type Header struct {
	ByteOrder     ByteOrder `json:"byteOrder"`
	Classic       bool      `json:"classic"`
	BigTIFF       bool      `json:"bigTIFF"`
	Magic         uint16    `json:"magic"`
	FirstIFD      int64     `json:"firstIFD"`
	ByteRange     ByteRange `json:"byteRange"`
	FirstIFDRange ByteRange `json:"firstIFDRange"`
}

type Tag struct {
	ID           string    `json:"id"`
	IFDID        string    `json:"ifdId"`
	Tag          uint16    `json:"tag"`
	Name         string    `json:"name"`
	EntryRange   ByteRange `json:"entryRange"`
	ValueRange   ByteRange `json:"valueRange"`
	External     bool      `json:"external"`
	ValuePresent bool      `json:"valuePresent"`
	Raw          RawValue  `json:"raw"`
}

type IFD struct {
	ID         string    `json:"id"`
	Offset     int64     `json:"offset"`
	ByteRange  ByteRange `json:"byteRange"`
	EntryCount int       `json:"entryCount"`
	NextOffset int64     `json:"nextOffset"`
	NextRange  ByteRange `json:"nextRange"`
	Shared     bool      `json:"shared"`
}

type Edge struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	SourceID  string    `json:"sourceId"`
	TargetID  string    `json:"targetId"`
	Offset    int64     `json:"offset"`
	TagID     string    `json:"tagId,omitempty"`
	ByteRange ByteRange `json:"byteRange"`
}

type Block struct {
	ID          string     `json:"id"`
	IFDID       string     `json:"ifdId"`
	Kind        string     `json:"kind"`
	Index       int        `json:"index"`
	Column      int        `json:"column"`
	Row         int        `json:"row"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	ImageWidth  int        `json:"imageWidth"`
	ImageHeight int        `json:"imageHeight"`
	ByteRange   *ByteRange `json:"byteRange,omitempty"`
	State       string     `json:"state"`
	Overlaps    []string   `json:"overlaps,omitempty"`
	Diagnostics []string   `json:"diagnostics,omitempty"`
}

type GeoKey struct {
	ID             string   `json:"id"`
	IFDID          string   `json:"ifdId"`
	Key            uint16   `json:"key"`
	Name           string   `json:"name"`
	LocationTag    int64    `json:"locationTag"`
	Count          int      `json:"count"`
	ValueOrOffset  int64    `json:"valueOrOffset"`
	ResolvedValue  string   `json:"resolvedValue"`
	RawValues      []string `json:"rawValues"`
	DuplicateIndex int      `json:"duplicateIndex"`
}

type Quantity struct {
	Raw        string  `json:"raw"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	Basis      string  `json:"basis"`
	Converted  bool    `json:"converted"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

type TransformCandidate struct {
	ID             string     `json:"id"`
	IFDID          string     `json:"ifdId"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	Coefficients   []float64  `json:"coefficients,omitempty"`
	Tiepoint       []Quantity `json:"tiepoint,omitempty"`
	Scale          []Quantity `json:"scale,omitempty"`
	SourceTagIDs   []string   `json:"sourceTagIds"`
	RejectedReason string     `json:"rejectedReason,omitempty"`
}

type GeoDirectory struct {
	IFDID    string    `json:"ifdId"`
	Version  int       `json:"version"`
	Revision string    `json:"revision"`
	KeyRange ByteRange `json:"keyRange"`
	Keys     []GeoKey  `json:"keys"`
}

type Finding struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Severity  string     `json:"severity"`
	ObjectID  string     `json:"objectId"`
	Message   string     `json:"message"`
	ByteRange *ByteRange `json:"byteRange,omitempty"`
}

type Budget struct {
	MaximumIFDVisits int  `json:"maximumIfdVisits"`
	IFDVisits        int  `json:"ifdVisits"`
	Remaining        int  `json:"remaining"`
	Exhausted        bool `json:"exhausted"`
}

type Graph struct {
	DocumentID          string               `json:"documentId"`
	FileLength          int64                `json:"fileLength"`
	Header              Header               `json:"header"`
	IFDs                []IFD                `json:"ifds"`
	Tags                []Tag                `json:"tags"`
	Edges               []Edge               `json:"edges"`
	Blocks              []Block              `json:"blocks"`
	GeoDirectories      []GeoDirectory       `json:"geoDirectories"`
	TransformCandidates []TransformCandidate `json:"transformCandidates"`
	Findings            []Finding            `json:"findings"`
	Budget              Budget               `json:"budget"`
	AlgorithmVersion    string               `json:"algorithmVersion"`
}

type Input struct {
	DocumentID   string
	Bytes        []byte
	MaxIFDVisits int
}
