package geo

// Unit describes a measure together with its reference system. Every
// displayed domain quantity carries one.
type Unit struct {
	Kind     string  `json:"kind"` // "linear" | "angular" | "dimensionless"
	Name     string  `json:"name"`
	Symbol   string  `json:"symbol"`
	ToSI     float64 `json:"toSI,omitempty"` // meters or radians; 0 when unknown
	Known    bool    `json:"known"`
}

var angularUnits = map[uint16]Unit{
	9101: {Kind: "angular", Name: "radian", Symbol: "rad", ToSI: 1, Known: true},
	9102: {Kind: "angular", Name: "degree", Symbol: "deg", ToSI: 0.017453292519943295, Known: true},
	9103: {Kind: "angular", Name: "arc-minute", Symbol: "ft. angle", ToSI: 0.017453292519943295 / 60, Known: true},
	9104: {Kind: "angular", Name: "arc-second", Symbol: "in. angle", ToSI: 0.017453292519943295 / 3600, Known: true},
	9105: {Kind: "angular", Name: "grad", Symbol: "grad", ToSI: 0.015707963267948966, Known: true},
}

var linearUnits = map[uint16]Unit{
	9001: {Kind: "linear", Name: "metre", Symbol: "m", ToSI: 1, Known: true},
	9002: {Kind: "linear", Name: "foot", Symbol: "ft", ToSI: 0.3048, Known: true},
	9003: {Kind: "linear", Name: "US survey foot", Symbol: "usft", ToSI: 1200.0 / 3937.0, Known: true},
	9006: {Kind: "linear", Name: "kilometre", Symbol: "km", ToSI: 1000, Known: true},
}

func AngularUnit(code uint16) Unit {
	if u, ok := angularUnits[code]; ok {
		return u
	}
	return Unit{Kind: "angular", Name: "unknown angular unit", Symbol: "?", Known: false}
}

func LinearUnit(code uint16) Unit {
	if u, ok := linearUnits[code]; ok {
		return u
	}
	return Unit{Kind: "linear", Name: "unknown linear unit", Symbol: "?", Known: false}
}

// CRSLabel resolves a small set of coordinate reference codes. Unknown codes
// remain labeled by raw value rather than guessed.
type CRSRef struct {
	Code    int    `json:"code"`
	Type    string `json:"type"` // "geographic" | "projected" | "unknown"
	Label   string `json:"label"`
	Angular Unit   `json:"angularUnit,omitempty"`
	Linear  Unit   `json:"linearUnit,omitempty"`
}

func ResolveCRS(geog, proj uint16, geogAngular, projLinear uint16) CRSRef {
	switch {
	case proj >= 32601 && proj <= 32660:
		return CRSRef{Code: int(proj), Type: "projected",
			Label: "WGS 84 / UTM zone " + itoa(int(proj-32600)) + "N",
			Linear: LinearUnit(pickLinear(projLinear))}
	case proj >= 32701 && proj <= 32760:
		return CRSRef{Code: int(proj), Type: "projected",
			Label: "WGS 84 / UTM zone " + itoa(int(proj-32700)) + "S",
			Linear: LinearUnit(pickLinear(projLinear))}
	case proj != 0:
		return CRSRef{Code: int(proj), Type: "projected",
			Label: "projected CRS code " + itoa(int(proj)),
			Linear: LinearUnit(pickLinear(projLinear))}
	case geog == 4326:
		return CRSRef{Code: 4326, Type: "geographic", Label: "WGS 84",
			Angular: AngularUnit(pickAngular(geogAngular))}
	case geog != 0:
		return CRSRef{Code: int(geog), Type: "geographic",
			Label: "geographic CRS code " + itoa(int(geog)),
			Angular: AngularUnit(pickAngular(geogAngular))}
	}
	return CRSRef{Type: "unknown", Label: "unspecified CRS"}
}

func pickAngular(c uint16) uint16 {
	if c == 0 {
		return 9102
	}
	return c
}

func pickLinear(c uint16) uint16 {
	if c == 0 {
		return 9001
	}
	return c
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
