package binding

// ID is a stable binding identifier.
type ID string

// Engine is a compile-time placeholder for control-to-action evaluation.
type Engine struct{}

// NewEngine returns an empty binding engine.
func NewEngine() *Engine {
	return &Engine{}
}
