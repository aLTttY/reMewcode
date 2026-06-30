package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type workspaceTool struct {
	root string
}

func newWorkspaceTool(root string) workspaceTool {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return workspaceTool{root: filepath.Clean(abs)}
}

func (t workspaceTool) resolvePath(value any) (string, string, error) {
	raw, err := stringArg(map[string]any{"path": value}, "path")
	if err != nil {
		return "", "", err
	}
	if raw == "" {
		return "", "", errors.New("path is required")
	}
	full := raw
	if !filepath.IsAbs(full) {
		full = filepath.Join(t.root, raw)
	}
	full, err = filepath.Abs(full)
	if err != nil {
		return "", "", err
	}
	full = filepath.Clean(full)
	if err := ensureInside(t.root, full); err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(t.root, full)
	if err != nil {
		return "", "", err
	}
	return full, filepath.ToSlash(rel), nil
}

func ensureInside(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path %q is outside workspace %q", target, root)
	}
	return nil
}

type readFileTool struct {
	workspaceTool
}

func newReadFileTool(root string) Tool {
	return readFileTool{workspaceTool: newWorkspaceTool(root)}
}

func (t readFileTool) Name() string { return "read_file" }

func (t readFileTool) Description() string {
	return "Read a UTF-8 text file from the current workspace."
}

func (t readFileTool) Parameters() map[string]any {
	return objectSchema(map[string]any{
		"path": map[string]any{"type": "string", "description": "Workspace-relative file path."},
	}, []string{"path"})
}

func (t readFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, rel, err := t.resolvePath(args["path"])
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": rel, "content": string(data)}, nil
}

type writeFileTool struct {
	workspaceTool
}

func newWriteFileTool(root string) Tool {
	return writeFileTool{workspaceTool: newWorkspaceTool(root)}
}

func (t writeFileTool) Name() string { return "write_file" }

func (t writeFileTool) Description() string {
	return "Write UTF-8 text content to a file in the current workspace, creating parent directories when needed."
}

func (t writeFileTool) Parameters() map[string]any {
	return objectSchema(map[string]any{
		"path":    map[string]any{"type": "string", "description": "Workspace-relative file path."},
		"content": map[string]any{"type": "string", "description": "Full file content to write."},
	}, []string{"path", "content"})
}

func (t writeFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, rel, err := t.resolvePath(args["path"])
	if err != nil {
		return nil, err
	}
	content, err := stringArg(args, "content")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		return nil, err
	}
	return map[string]any{"path": rel, "bytes": len([]byte(content))}, nil
}

type editFileTool struct {
	workspaceTool
}

func newEditFileTool(root string) Tool {
	return editFileTool{workspaceTool: newWorkspaceTool(root)}
}

func (t editFileTool) Name() string { return "edit_file" }

func (t editFileTool) Description() string {
	return "Edit a workspace file by replacing exactly one occurrence of old_text with new_text."
}

func (t editFileTool) Parameters() map[string]any {
	return objectSchema(map[string]any{
		"path":     map[string]any{"type": "string", "description": "Workspace-relative file path."},
		"old_text": map[string]any{"type": "string", "description": "Existing text that must match exactly once."},
		"new_text": map[string]any{"type": "string", "description": "Replacement text."},
	}, []string{"path", "old_text", "new_text"})
}

func (t editFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, rel, err := t.resolvePath(args["path"])
	if err != nil {
		return nil, err
	}
	oldText, err := stringArg(args, "old_text")
	if err != nil {
		return nil, err
	}
	newText, err := stringArg(args, "new_text")
	if err != nil {
		return nil, err
	}
	if oldText == "" {
		return nil, errors.New("old_text must not be empty")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return nil, errors.New("old_text was not found")
	}
	if count > 1 {
		return nil, fmt.Errorf("old_text matched %d times; provide a more specific replacement", count)
	}
	updated := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(full, []byte(updated), 0644); err != nil {
		return nil, err
	}
	return map[string]any{
		"path":          rel,
		"bytes_written": len([]byte(updated)),
	}, nil
}

type runCommandTool struct {
	workspaceTool
}

func newRunCommandTool(root string) Tool {
	return runCommandTool{workspaceTool: newWorkspaceTool(root)}
}

func (t runCommandTool) Name() string { return "run_command" }

func (t runCommandTool) Description() string {
	return "Run a non-interactive shell command in the current workspace and return stdout, stderr, and exit code."
}

func (t runCommandTool) Parameters() map[string]any {
	return objectSchema(map[string]any{
		"command":         map[string]any{"type": "string", "description": "Command to run."},
		"workdir":         map[string]any{"type": "string", "description": "Optional workspace-relative working directory."},
		"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds, capped at 30."},
	}, []string{"command"})
}

func (t runCommandTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	command, err := stringArg(args, "command")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("command must not be empty")
	}
	timeout := time.Duration(numberArg(args, "timeout_seconds", 30)) * time.Second
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workdir := t.root
	if raw, ok := args["workdir"]; ok && raw != nil {
		full, _, err := t.resolvePath(raw)
		if err != nil {
			return nil, err
		}
		workdir = full
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = workdir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	result := map[string]any{
		"command":   command,
		"workdir":   workdir,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"exit_code": 0,
	}
	if ctx.Err() == context.DeadlineExceeded {
		result["timed_out"] = true
		return nil, &ExecutionError{Message: fmt.Sprintf("command timed out after %s", timeout), Value: result}
	}
	if runErr != nil {
		exitCode := 1
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		result["exit_code"] = exitCode
		return nil, &ExecutionError{Message: fmt.Sprintf("command exited with code %d", exitCode), Value: result}
	}
	return result, nil
}

type findFilesTool struct {
	workspaceTool
}

func newFindFilesTool(root string) Tool {
	return findFilesTool{workspaceTool: newWorkspaceTool(root)}
}

func (t findFilesTool) Name() string { return "find_files" }

func (t findFilesTool) Description() string {
	return "Find files in the current workspace by glob pattern."
}

func (t findFilesTool) Parameters() map[string]any {
	return objectSchema(map[string]any{
		"pattern":     map[string]any{"type": "string", "description": "Glob pattern such as *.go or internal/**/*.go."},
		"max_results": map[string]any{"type": "number", "description": "Maximum number of files to return."},
	}, []string{"pattern"})
}

func (t findFilesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pattern, err := stringArg(args, "pattern")
	if err != nil {
		return nil, err
	}
	if pattern == "" {
		pattern = "*"
	}
	maxResults := int(numberArg(args, "max_results", 100))
	if maxResults <= 0 || maxResults > 500 {
		maxResults = 100
	}
	matcher, err := newGlobMatcher(pattern)
	if err != nil {
		return nil, err
	}
	matches := []string{}
	err = filepath.WalkDir(t.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) && path != t.root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(t.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if matcher(rel) {
			matches = append(matches, rel)
		}
		if len(matches) >= maxResults {
			return errEnoughResults
		}
		return nil
	})
	if errors.Is(err, errEnoughResults) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return map[string]any{"pattern": pattern, "files": matches}, nil
}

type searchCodeTool struct {
	workspaceTool
}

func newSearchCodeTool(root string) Tool {
	return searchCodeTool{workspaceTool: newWorkspaceTool(root)}
}

func (t searchCodeTool) Name() string { return "search_code" }

func (t searchCodeTool) Description() string {
	return "Search text content in workspace files and return matching file paths and line numbers."
}

func (t searchCodeTool) Parameters() map[string]any {
	return objectSchema(map[string]any{
		"query":          map[string]any{"type": "string", "description": "Literal text to search for."},
		"path_pattern":   map[string]any{"type": "string", "description": "Optional glob pattern limiting searched files."},
		"case_sensitive": map[string]any{"type": "boolean", "description": "Whether matching is case-sensitive."},
		"max_results":    map[string]any{"type": "number", "description": "Maximum number of matches to return."},
	}, []string{"query"})
}

func (t searchCodeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	query, err := stringArg(args, "query")
	if err != nil {
		return nil, err
	}
	if query == "" {
		return nil, errors.New("query must not be empty")
	}
	pattern, _ := stringArg(args, "path_pattern")
	if pattern == "" {
		pattern = "**"
	}
	matcher, err := newGlobMatcher(pattern)
	if err != nil {
		return nil, err
	}
	caseSensitive := boolArg(args, "case_sensitive", false)
	maxResults := int(numberArg(args, "max_results", 100))
	if maxResults <= 0 || maxResults > 500 {
		maxResults = 100
	}
	needle := query
	if !caseSensitive {
		needle = strings.ToLower(needle)
	}
	results := []map[string]any{}
	err = filepath.WalkDir(t.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) && path != t.root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(t.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !matcher(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.Contains(data, []byte{0}) {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			haystack := line
			if !caseSensitive {
				haystack = strings.ToLower(haystack)
			}
			if strings.Contains(haystack, needle) {
				results = append(results, map[string]any{
					"path": rel,
					"line": i + 1,
					"text": strings.TrimRight(line, "\r"),
				})
				if len(results) >= maxResults {
					return errEnoughResults
				}
			}
		}
		return nil
	})
	if errors.Is(err, errEnoughResults) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"query": query, "matches": results}, nil
}

var errEnoughResults = errors.New("enough results")

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".gocache", ".gomodcache":
		return true
	default:
		return false
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringArg(args map[string]any, name string) (string, error) {
	value, ok := args[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%s is required", name)
	}
	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return str, nil
}

func boolArg(args map[string]any, name string, fallback bool) bool {
	value, ok := args[name]
	if !ok || value == nil {
		return fallback
	}
	boolValue, ok := value.(bool)
	if !ok {
		return fallback
	}
	return boolValue
}

func numberArg(args map[string]any, name string, fallback float64) float64 {
	value, ok := args[name]
	if !ok || value == nil {
		return fallback
	}
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}

func newGlobMatcher(pattern string) (func(string) bool, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" || pattern == "**" {
		return func(string) bool { return true }, nil
	}
	if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
		return func(rel string) bool {
			ok, _ := filepath.Match(pattern, filepath.Base(rel))
			return ok
		}, nil
	}
	regex, err := globToRegexp(pattern)
	if err != nil {
		return nil, err
	}
	return func(rel string) bool {
		return regex.MatchString(filepath.ToSlash(rel))
	}, nil
}

func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
