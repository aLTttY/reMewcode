package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistrySchemasByProtocol(t *testing.T) {
	registry := NewRegistryAt(t.TempDir())

	openAISchemas := registry.Schemas("deepseek")
	if len(openAISchemas) == 0 {
		t.Fatal("schemas is empty")
	}
	if openAISchemas[0]["type"] != "function" {
		t.Fatalf("deepseek schema type = %#v, want function", openAISchemas[0]["type"])
	}

	anthropicSchemas := registry.Schemas("anthropic")
	if _, ok := anthropicSchemas[0]["input_schema"]; !ok {
		t.Fatalf("anthropic schema missing input_schema: %#v", anthropicSchemas[0])
	}
}

func TestFileToolsReadWriteAndEdit(t *testing.T) {
	root := t.TempDir()
	registry := NewRegistryAt(root)
	ctx := context.Background()

	write := registry.Execute(ctx, "write_file", map[string]any{
		"path":    "notes/example.txt",
		"content": "hello world\n",
	})
	if !write.OK {
		t.Fatalf("write_file failed: %s", write.Error)
	}

	read := registry.Execute(ctx, "read_file", map[string]any{"path": "notes/example.txt"})
	if !read.OK {
		t.Fatalf("read_file failed: %s", read.Error)
	}
	payload := read.Value.(map[string]any)
	if payload["content"] != "hello world\n" {
		t.Fatalf("content = %#v", payload["content"])
	}

	edit := registry.Execute(ctx, "edit_file", map[string]any{
		"path":     "notes/example.txt",
		"old_text": "hello",
		"new_text": "hi",
	})
	if !edit.OK {
		t.Fatalf("edit_file failed: %s", edit.Error)
	}
	data, err := os.ReadFile(filepath.Join(root, "notes", "example.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi world\n" {
		t.Fatalf("updated content = %q", string(data))
	}
}

func TestEditFileRequiresUniqueMatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dup.txt")
	if err := os.WriteFile(path, []byte("x\nx\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result := NewRegistryAt(root).Execute(context.Background(), "edit_file", map[string]any{
		"path":     "dup.txt",
		"old_text": "x",
		"new_text": "y",
	})
	if result.OK {
		t.Fatal("edit_file succeeded, want duplicate match error")
	}
	if !strings.Contains(result.Error, "matched 2 times") {
		t.Fatalf("error = %q", result.Error)
	}
}

func TestFindFilesAndSearchCode(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "main.go"), []byte("package main\nfunc hello() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("hello docs\n"), 0644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistryAt(root)

	found := registry.Execute(context.Background(), "find_files", map[string]any{"pattern": "internal/**/*.go"})
	if !found.OK {
		t.Fatalf("find_files failed: %s", found.Error)
	}
	files := found.Value.(map[string]any)["files"].([]string)
	if len(files) != 1 || files[0] != "internal/main.go" {
		t.Fatalf("files = %#v", files)
	}

	search := registry.Execute(context.Background(), "search_code", map[string]any{
		"query":        "hello",
		"path_pattern": "*.go",
	})
	if !search.OK {
		t.Fatalf("search_code failed: %s", search.Error)
	}
	matches := search.Value.(map[string]any)["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["path"] != "internal/main.go" {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestToolResultJSON(t *testing.T) {
	result := ToolResult{Tool: "read_file", OK: false, Error: "missing"}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result.JSON()), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["ok"] != false || decoded["error"] != "missing" {
		t.Fatalf("decoded = %#v", decoded)
	}
}
