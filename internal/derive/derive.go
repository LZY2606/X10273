// Package derive turns parsed TIFF structures into review-facing derived
// objects: pixel block maps, geo-transform candidates and evidence. Every
// derived object carries an algorithm version, parameter fingerprint and
// direct sources so results can be invalidated locally.
package derive

const (
	AlgorithmVersion = "derive-1.1.0"
	// DefaultBudget is the parser access budget used unless parameters change.
	DefaultIfdBudget   = 64
	DefaultValueBudget = 1024
)

// Params are the tunable parse/derive parameters. Their fingerprint marks
// every derived value.
type Params struct {
	IfdBudget   int `json:"ifdBudget"`
	ValueBudget int `json:"valueBudget"`
}

func DefaultParams() Params {
	return Params{IfdBudget: DefaultIfdBudget, ValueBudget: DefaultValueBudget}
}

func (p Params) Fingerprint() string {
	return "ifdb=" + itoa(p.IfdBudget) + ";valb=" + itoa(p.ValueBudget)
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
	q := len(b)
	for i > 0 {
		q--
		b[q] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		q--
		b[q] = '-'
	}
	return string(b[q:])
}
