# ch02: 让 AI 开口说话 Checklist

> 所有条目必须可勾选、可观测。验收方式写在每项后面的括号里。

## 1. 实现完整性

- [ ] `Client` 接口在 `internal/llm/client.go:11-13` 实现，签名 `Stream(ctx, conv, tools) (<-chan StreamEvent, <-chan error)`（`grep -n 'type Client interface' internal/llm/client.go`）。
- [ ] `MaxTokensSetter` 接口在 `internal/llm/client.go:15-17` 实现（`grep -n 'type MaxTokensSetter' internal/llm/client.go`）。
- [ ] `NewClient` 在 `internal/llm/client.go:19-28` 按 protocol ∈ {anthropic, openai} 分流，未知 protocol 返回 `fmt.Errorf("unknown protocol: %s", ...)`。
- [ ] 7 个流式事件类型 + `UsageInfo` 在 `internal/llm/events.go:1-34` 齐全，全部绑定 `streamEvent()` 私有方法。
- [ ] `LLMError`/`AuthenticationError`/`RateLimitError{RetryAfter}`/`NetworkError`/`ContextTooLongError` 在 `internal/llm/errors.go:3-32` 齐全。
- [ ] `supportsAdaptiveThinking` 在 `internal/llm/anthropic.go:21-33` 严格按 `claude-opus-4-` / `claude-sonnet-4-` 且 minor ≥ '6' 判定。
- [ ] `anthropicClient.Stream` 在 `internal/llm/anthropic.go:71-246` 实现：
 - [ ] SSE 读取在独立 goroutine（`readNext`，`anthropic.go:139-141`）；
 - [ ] `select` 含 `ctx.Done()` / `idle.C` / `nextCh` 三路（`anthropic.go:149-157`）；
 - [ ] `accMessage.Accumulate(event)` 累积消息（`anthropic.go:172`）；
 - [ ] 在 ContentBlockStart 处分别识别 `thinking` / `tool_use`；ContentBlockDelta 处分别识别 `ThinkingDelta` / `SignatureDelta` / `TextDelta` / `InputJSONDelta`；
 - [ ] StreamEnd 携带 StopReason（默认 `end_turn`）与 `UsageInfo`。
- [ ] `buildAnthropicMessages` 在 `internal/llm/anthropic.go:248-297` 处理 assistant 的 thinking blocks / text / tool_use 合并，并把 tool_results 包成 user 消息。
- [ ] `classifyAnthropicError` 在 `internal/llm/anthropic.go:299-325` 覆盖 413 / `prompt is too long` / `AuthenticationError` / `RateLimitError`（取 `Retry-After` 头）/ default。
- [ ] `openaiClient.Stream` 在 `internal/llm/openai.go:59-207` 处理 `response.output_text.delta`、`response.output_item.added`（function_call / reasoning）、`response.reasoning_summary_text.delta/done`、`response.function_call_arguments.delta/done`、`response.completed`。
- [ ] OpenAI thinking=true 时设置 `reasoning.effort=high` / `summary=detailed` / `include=[reasoning.encrypted_content]`（`internal/llm/openai.go:91-99`）。
- [ ] `classifyOpenAIError` 在 `internal/llm/openai.go:262-288` 处理 413 + 400/`context_length_exceeded`、401、429、default；`containsContextLengthError` 在 `:290` 覆盖三种关键字。
- [ ] `NewModelResolver` 在 `internal/llm/model_resolver.go:11-21` 暴露短名 → ID 映射闭包。
- [ ] `conversation.Message{Role, Content, ThinkingBlocks, ToolUses, ToolResults}` 在 `internal/conversation/conversation.go:22-28` 定义。
- [ ] `Manager` 8 个 Add 方法 + GetMessages + Serialize 在 `internal/conversation/conversation.go:34-196` 齐全。
- [ ] `AddSystemReminder` 包裹 `<system-reminder>\n{content}\n</system-reminder>`（`internal/conversation/conversation.go:93-98`）。
- [ ] `serializeAnthropic` 合并同角色连续文本消息以维持 user/assistant 交替（`internal/conversation/conversation.go:142-160`）。

## 2. 接入完整性（必查，杜绝死代码）

- [ ] `llm.NewClient` 至少 4 个非测试调用方（`grep -rn "llm.NewClient" --include="*.go" /Users/codemelo/mewcode | grep -v _test.go` 命中 `internal/tui/tui.go:352`、`:714`、`cmd/mewcode/teammate.go:82`）。
- [ ] `conversation.NewManager` 至少 6 个非测试调用方（`grep -rn "conversation.NewManager" --include="*.go" /Users/codemelo/mewcode | grep -v _test.go` 命中 TUI/Compact/Agents/Teammate 等）。
- [ ] `agent.go:105` 实际调用 `a.Client.Stream(ctx, conv, toolSchemas)`，证明 Client 接口接到 Agent Loop。
- [ ] `agent.go:117-142` 消费 `ThinkingDelta`/`ThinkingComplete`/`TextDelta`/`ToolCallStart`/`ToolCallDelta`/`ToolCallComplete`/`StreamEnd` 七种事件，无未处理事件类型遗漏。
- [ ] `agent.go:172/180/192/205` 通过 `conv.AddAssistantFull(text, thinkingBlocks, toolUses)` 把 thinking 与 tool 写回历史，保证下一轮能回放 signature。
- [ ] `NewModelResolver` 在 `internal/tui/tui.go:546` 被 `agents.AgentTool` 装配时使用（`grep -rn "NewModelResolver" --include="*.go"`）。
- [ ] `LLMError` / `ContextTooLongError` / `RateLimitError` / `NetworkError` 在 `internal/agent/agent.go:264-288` 的 `handleStreamError` 中被 `errors.As` 消费，错误链未断。

## 3. 编译与测试

- [ ] `go build ./...` 通过。
- [ ] `go test ./internal/llm/...` 通过：6 个 thinking_test 全绿（`go test -run 'Test.*Thinking' ./internal/llm/...`）。
- [ ] `go vet ./internal/llm/... ./internal/conversation/...` 无警告。

## 4. 端到端验证

- [ ] TUI 启动后发送 `hello`，对话窗口逐 token 渲染流式回复——证明 `TextDelta` 通道接到 `internal/tui/tui.go` 的事件渲染。
- [ ] 模型为 `claude-sonnet-4-6`（或更新）时，配置 `thinking: true` 后能在对话区看到 thinking 文本流（`ThinkingDelta` → `tui` 渲染），证明 adaptive thinking 接通。
- [ ] 提供故意失败的 API key 后 TUI 显示 `Invalid API key: ...`（`AuthenticationError` 路径），证明错误分类生效。
- [ ] 留存证据: `internal/llm/thinking_test.go` 在 `go test -v` 下输出 `Official model → adaptive: ...` 等日志行（`thinking_test.go:94`、`:127`）。

## 5. 文档

- [ ] spec.md / tasks.md / checklist.md 三件套齐全（`/Users/codemelo/mewcode/specs/go/ch02/`）。