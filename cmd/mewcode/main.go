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
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("DEEPSEEK_API_KEY is not set")
	}

	provider := config.Provider{
		Name:      "deepseek",
		Protocol:  "deepseek",
		Model:     getenvDefault("DEEPSEEK_MODEL", "deepseek-chat"),
		APIKey:    apiKey,
		BaseURL:   getenvDefault("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		MaxTokens: 2048,
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
	fmt.Println("DeepSeek chat ready. Type /exit to quit.")
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
		conv.AddUser(prompt)
		fmt.Print("assistant: ")
		if err := streamAssistant(client, conv); err != nil {
			return err
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
