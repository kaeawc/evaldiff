package index

// ValueKind discriminates between extractable and non-extractable argument
// values. Static analysis can only see literals; everything else is recorded
// as Dynamic so the diff stage knows it changed but cannot compare contents.
type ValueKind int

const (
	ValueMissing ValueKind = iota // arg was not provided
	ValueLiteral                  // arg was a string/int/bool literal we extracted
	ValueDynamic                  // arg was an expression we cannot statically resolve
)

// Value is the resolved (or unresolved) value of a single agent kwarg.
// Str is populated when Kind == ValueLiteral; Source is the raw source text
// for both literal and dynamic values (handy for diffs and debugging).
type Value struct {
	Kind   ValueKind
	Str    string
	Source string
}

func (v Value) IsLiteral() bool { return v.Kind == ValueLiteral }
func (v Value) IsDynamic() bool { return v.Kind == ValueDynamic }
func (v Value) IsMissing() bool { return v.Kind == ValueMissing }
