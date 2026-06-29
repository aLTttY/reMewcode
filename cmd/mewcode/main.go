package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/codemelo/mewcode/internal/config"
	"github.com/codemelo/mewcode/internal/conversation"
	"github.com/codemelo/mewcode/internal/llm"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: mewcode ask <message> | mewcode chat")
	}
	loadDotEnv(".env")
	apiKey := os.Getenv("STONEAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("STONEAI_API_KEY is not set")
	}

	provider := config.Provider{
		Name:      "stoneai",
		Protocol:  "deepseek",
		Model:     getenvDefault("STONEAI_MODEL", "claude-opus-4-8"),
		APIKey:    apiKey,
		BaseURL:   getenvDefault("STONEAI_BASE_URL", "https://www.stoneai.fun/v1"),
		MaxTokens: getenvIntDefault("STONEAI_MAX_TOKENS", 0),
	}
	client, err := llm.NewClient(&provider, "You are a concise coding assistant.")
	if err != nil {
		return err
	}

	conv := conversation.NewManager()
	switch args[0] {
	case "ask":
		if len(args) < 2 {
			return fmt.Errorf("usage: mewcode ask <message>")
		}
		return askOnce(client, conv, strings.Join(args[1:], " "))
	case "chat":
		return chatLoop(client, conv)
	default:
		return fmt.Errorf("usage: mewcode ask <message> | mewcode chat")
	}
}

func askOnce(client llm.Client, conv *conversation.Manager, prompt string) error {
	conv.AddUser(prompt)
	return streamAssistant(client, conv)
}

func chatLoop(client llm.Client, conv *conversation.Manager) error {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("StoneAI Claude chat ready. Type /exit to quit.")
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		prompt := strings.TrimSpace(scanner.Text())
		if prompt == "" {
			continue
		}
		if prompt == "/exit" || prompt == "/quit" {
			return nil
		}
		checkpoint := conv.Len()
		conv.AddUser(prompt)
		fmt.Print("assistant: ")
		if err := streamAssistant(client, conv); err != nil {
			conv.Truncate(checkpoint)
			fmt.Printf("\nerror: %v\n", err)
			continue
		}
	}
	return scanner.Err()
}

func streamAssistant(client llm.Client, conv *conversation.Manager) error {
	events, errs := client.Stream(context.Background(), conv, nil)
	var text strings.Builder
	for event := range events {
		switch e := event.(type) {
		case llm.TextDelta:
			fmt.Print(e.Text)
			text.WriteString(e.Text)
		case llm.ThinkingDelta:
		case llm.ThinkingComplete:
		case llm.StreamEnd:
			fmt.Println()
		}
	}
	if err := <-errs; err != nil {
		return err
	}
	if text.Len() > 0 {
		conv.AddAssistant(text.String())
	}
	return nil
}

func getenvDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getenvIntDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
}
