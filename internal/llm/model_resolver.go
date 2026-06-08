package llm

import "github.com/codemelo/mewcode/internal/config"

var modelAliases = map[string]string{
	"haiku":  "claude-3-5-haiku-latest",
	"sonnet": "claude-sonnet-4-6",
	"opus":   "claude-opus-4-6",
}

func NewModelResolver(baseCfg config.Provider) func(shortName string) (Client, error) {
	return func(shortName string) (Client, error) {
		model, ok := modelAliases[shortName]
		if !ok {
			model = shortName
		}
		cfg := baseCfg.WithModel(model)
		return NewClient(&cfg, "")
	}
}
