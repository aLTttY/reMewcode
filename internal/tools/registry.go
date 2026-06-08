package tools

type Registry struct {
	schemas []map[string]any
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) AddSchema(schema map[string]any) {
	r.schemas = append(r.schemas, schema)
}

func (r *Registry) Schemas() []map[string]any {
	out := make([]map[string]any, len(r.schemas))
	copy(out, r.schemas)
	return out
}
