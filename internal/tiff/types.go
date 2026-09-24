// Package tiff 实现本地优先的 TIFF/BigTIFF/GeoTIFF 解析器。
// 解析器保留大小端、classic/BigTIFF、tag 原值、外部值偏移、IFD 链、
// tile/strip 范围与 GeoKey 版本,并把全部判断依据输出为机器可读取事件。
package tiff

import "fmt"

// ByteOrder 保留文件头声明的大小端。
type ByteOrder string

const (
	LittleEndian ByteOrder = "II"
	BigEndian    ByteOrder = "MM"
)

// Variant 区分 classic TIFF 与 BigTIFF(宽偏移)。
type Variant string

const (
	Classic Variant = "classic"
	BigTIFF Variant = "bigtiff"
)

// Event 是机器可读取的判定依据事件。
type Event struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// Tag 保留一个 IFD 条目的原值与字节范围。
type Tag struct {
	ID         uint16   `json:"id"`
	Name       string   `json:"name"`
	Type       uint16   `json:"type"`
	TypeName   string   `json:"typeName"`
	Count      uint64   `json:"count"`
	Inline     bool     `json:"inline"`      // 值是否内联在条目内
	ValueField uint64   `json:"valueField"`  // 值字段原始内容(外部值时为偏移)
	EntryRange [2]uint64 `json:"entryRange"` // 条目在文件中的字节范围
	ValueRange [2]uint64 `json:"valueRange"` // 值字节在文件中的范围
	Raw        []byte   `json:"raw,omitempty"`
	Uints      []uint64 `json:"uints,omitempty"`
	Doubles    []float64 `json:"doubles,omitempty"`
	Text       string   `json:"text,omitempty"`
	Diag       []string `json:"diag,omitempty"` // 无法转换时的诊断,值留在原位置
}

// Identity 返回该 tag 的稳定身份。
func (t Tag) Identity(ifdOffset uint64) string {
	return fmt.Sprintf("tag@%d/#%d", ifdOffset, t.ID)
}

// IFD 是目录图中的一个节点。
type IFD struct {
	Offset     uint64   `json:"offset"`
	Index      int      `json:"index"` // 访问顺序
	Visited    bool     `json:"visited"`
	Shared     bool     `json:"shared"`     // 被多个父节点引用的共享子目录
	LoopBack   bool     `json:"loopBack"`   // next 指针回指已访问节点
	Via        []string `json:"via"`        // 到达路径(main/subifd:tag)
	Tags       []Tag    `json:"tags"`
	NextOffset uint64   `json:"nextOffset"` // 原始 next 指针值
	Status     string   `json:"status"`     // ok | loop | shared | unvisited
	ByteRange  [2]uint64 `json:"byteRange"`
}

// Block 是 tile/strip 像素块,重叠与缺失显式呈现,不拼凑。
type Block struct {
	Index     int    `json:"index"`
	Kind      string `json:"kind"` // tile | strip
	Offset    uint64 `json:"offset"`
	ByteCount uint64 `json:"byteCount"`
	X0        uint32 `json:"x0"`
	Y0        uint32 `json:"y0"`
	X1        uint32 `json:"x1"`
	Y1        uint32 `json:"y1"`
	Status    string `json:"status"` // ok | overlap | missing | out_of_bounds
	Detail    string `json:"detail,omitempty"`
}

// Identity 返回像素块稳定身份。
func (b Block) Identity() string { return fmt.Sprintf("blk:%s#%d", b.Kind, b.Index) }

// GeoKey 是 GeoKeyDirectory 中的一条键。
type GeoKey struct {
	ID       uint16 `json:"id"`
	Name     string `json:"name,omitempty"`
	Location uint16 `json:"location"` // 0=内联, 34736=double 参数, 34737=ascii 参数
	Count    uint16 `json:"count"`
	Offset   uint16 `json:"offset"`
	Resolved string `json:"resolved"` // 解析后的展示值(保留原表示的说明)
}

// Geo 保留 GeoKey 目录版本与全部键。
type Geo struct {
	KeyDirectoryVersion int      `json:"keyDirectoryVersion"`
	KeyRevision         int      `json:"keyRevision"`
	MinorRevision       int      `json:"minorRevision"`
	NumberOfKeys        int      `json:"numberOfKeys"`
	Keys                []GeoKey `json:"keys"`
	Raw                 []uint16 `json:"raw"`
}

// Candidate 是地理变换候选,冲突时全部保留。
type Candidate struct {
	ID     string     `json:"id"`     // 稳定身份:hash(kind, 参数)
	Kind   string     `json:"kind"`   // tiepoint_scale | transformation
	Units  string     `json:"units"`  // 坐标基准说明
	Matrix [6]float64 `json:"matrix"` // 仿射: X=a*i+b*j+c, Y=d*i+e*j+f
	Source []string   `json:"source"` // 直接来源 tag 身份
	Status string     `json:"status"` // pending | consistent | conflict
	Note   string     `json:"note,omitempty"`
}

// Fragment 是派生层的直接来源片段,带内容哈希。
type Fragment struct {
	ID        string    `json:"id"` // header | ifd@<off> | geokeys | tiepoint | scale | transform | tiling
	ByteRange [2]uint64 `json:"byteRange"`
	SHA256    string    `json:"sha256"`
}

// BudgetInfo 记录访问预算使用情况。
type BudgetInfo struct {
	Limit     int  `json:"limit"`
	Used      int  `json:"used"`
	Exhausted bool `json:"exhausted"`
}

// Result 是一次解析的完整产出。
type Result struct {
	AlgoVersion   string            `json:"algoVersion"`
	ParamFP       string            `json:"paramFP"`
	ByteOrder     ByteOrder         `json:"byteOrder"`
	Variant       Variant           `json:"variant"`
	HeaderRange   [2]uint64         `json:"headerRange"`
	FirstIFD      uint64            `json:"firstIFD"`
	FileSize      int               `json:"fileSize"`
	IFDs          []*IFD            `json:"ifds"`
	Blocks        []Block           `json:"blocks"`
	Geo           *Geo              `json:"geo,omitempty"`
	Candidates    []Candidate       `json:"candidates"`
	Events        []Event           `json:"events"`
	Budget        BudgetInfo        `json:"budget"`
	Fragments     []Fragment        `json:"fragments"`
	ImageWidth    uint32            `json:"imageWidth"`
	ImageLength   uint32            `json:"imageLength"`
	Tiled         bool              `json:"tiled"`
	Diagnostics   []string          `json:"diagnostics,omitempty"`
}

// Options 是解析参数,参与参数指纹。
type Options struct {
	Budget int `json:"budget"` // IFD 访问预算
}

// DefaultBudget 是默认 IFD 访问预算。
const DefaultBudget = 64

// AlgoVersion 是解析算法版本。
const AlgoVersion = "tiffparse/v1"
