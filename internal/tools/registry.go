package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

const defaultToolTimeout = 30 * time.Second

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, args map[string]any) (any, error)
}

type ToolResult struct {
	Tool  string `json:"tool"`
	OK    bool   `json:"ok"`
	Value any    `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

type ExecutionError struct {
	Message string
	Value   any
}

func (e *ExecutionError) Error() string {
	return e.Message
}

type Registry struct {
	tools        map[string]Tool
	order        []string
	extraSchemas []map[string]any
	timeout      time.Duration
	root         string
}

func NewRegistry() *Registry {
	root, err := os.Getwd()
	if err != nil {
		root = "."
	}
	return NewRegistryAt(root)
}

func NewRegistryAt(root string) *Registry {
	r := &Registry{
		tools:   map[string]Tool{},
		timeout: defaultToolTimeout,
		root:    root,
	}
	r.RegisterCoreTools()
	return r
}

func (r *Registry) RegisterCoreTools() {
	r.Add(newReadFileTool(r.root))
	r.Add(newWriteFileTool(r.root))
	r.Add(newEditFileTool(r.root))
	r.Add(newRunCommandTool(r.root))
	r.Add(newFindFilesTool(r.root))
	r.Add(newSearchCodeTool(r.root))
}

func (r *Registry) Add(tool Tool) {
	if tool == nil || tool.Name() == "" {
		return
	}
	if _, exists := r.tools[tool.Name()]; !exists {
		r.order = append(r.order, tool.Name())
		sort.Strings(r.order)
	}
	r.tools[tool.Name()] = tool
}

func (r *Registry) AddSchema(schema map[string]any) {
	r.extraSchemas = append(r.extraSchemas, cloneMap(schema))
}

func (r *Registry) Schemas(protocol ...string) []map[string]any {
	selectedProtocol := ""
	if len(protocol) > 0 {
		selectedProtocol = protocol[0]
	}
	out := make([]map[string]any, 0, len(r.order)+len(r.extraSchemas))
	for _, name := range r.order {
		out = append(out, toolSchema(r.tools[name], selectedProtocol))
	}
	for _, schema := range r.extraSchemas {
		out = append(out, cloneMap(schema))
	}
	return out
}

func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) ToolResult {
	tool, ok := r.tools[name]
	if !ok {
		return ToolResult{Tool: name, OK: false, Error: fmt.Sprintf("unknown tool %q", name)}
	}
	if args == nil {
		args = map[string]any{}
	}
	timeout := r.timeout
	if timeout <= 0 {
		timeout = defaultToolTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	value, err := tool.Execute(ctx, args)
	if err != nil {
		result := ToolResult{Tool: name, OK: false, Error: err.Error()}
		var executionErr *ExecutionError
		if errors.As(err, &executionErr) {
			result.Value = executionErr.Value
		}
		return result
	}
	return ToolResult{Tool: name, OK: true, Value: value}
}

func (r ToolResult) JSON() string {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Sprintf(`{"tool":%q,"ok":false,"error":%q}`, r.Tool, err.Error())
	}
	return string(data)
}

func toolSchema(tool Tool, protocol string) map[string]any {
	base := map[string]any{
		"name":        tool.Name(),
		"description": tool.Description(),
		"parameters":  cloneMap(tool.Parameters()),
	}
	if protocol == "anthropic" {
		return map[string]any{
			"name":         base["name"],
			"description":  base["description"],
			"input_schema": base["parameters"],
		}
	}
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        base["name"],
			"description": base["description"],
			"parameters":  base["parameters"],
		},
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
