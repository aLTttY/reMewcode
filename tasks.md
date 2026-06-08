# ch02: 让 AI 开口说话 Tasks
## T4: 实现 Anthropic 客户端
- 影响文件: `internal/llm/anthropic.go`
- 依赖任务: T1, T2, T3
- 完成标准:
 - `internal/llm/anthropic.go:21` 实现 `supportsAdaptiveThinking(model)` 覆盖 4.6/4.7 但拒 4.5；
 - `internal/llm/anthropic.go:71` 实现 `Stream`，含 SSE 读 goroutine + idle.C(5min) + ctx.Done 三路 select；
 - `internal/llm/anthropic.go:248` 实现 `buildAnthropicMessages` 把 `conversation.Message` 序列化成 `[]anthropic.MessageParam`，含 thinking block / tool_use / tool_result；
 - `internal/llm/anthropic.go:299` 实现 `classifyAnthropicError` 按 413/auth/rate-limit/default 分支返回不同错误类型。

## T5: 实现 OpenAI Responses 客户端
- 影响文件: `internal/llm/openai.go`
- 依赖任务: T1, T2, T3
- 完成标准:
 - `internal/llm/openai.go:32` 实现 `newOpenAIClient`；
 - `internal/llm/openai.go:59` 实现 `Stream`，支持 reasoning effort=high/summary=detailed + `reasoning.encrypted_content` include；
 - `internal/llm/openai.go:209` 实现 `buildOpenAIInput` 把内部消息映射到 `responses.ResponseInputParam`；
 - `internal/llm/openai.go:262` 实现 `classifyOpenAIError`；`:290` 实现 `containsContextLengthError`。

## T6: 实现 Model Resolver（短名映射）
- 影响文件: `internal/llm/model_resolver.go`
- 依赖任务: T1
- 完成标准: `internal/llm/model_resolver.go:5-9` 定义 `modelAliases` map（haiku/sonnet/opus）；`:11` 暴露 `NewModelResolver(baseCfg)` 返回 `func(shortName) (Client, error)`。

## T7: 实现 `conversation.Manager` 与消息类型
- 影响文件: `internal/conversation/conversation.go`
- 依赖任务: 无
- 完成标准:
 - `internal/conversation/conversation.go:5-28` 定义 `ToolUseBlock`、`ToolResultBlock`、`ThinkingBlock`、`Message`；
 - `internal/conversation/conversation.go:30-99` 实现 `Manager` + 8 个 Add 方法（含 `AddSystemReminder` 包裹 `<system-reminder>` 标签）；
 - `internal/conversation/conversation.go:100-105` 实现 `GetMessages` 返回深拷贝；
 - `internal/conversation/conversation.go:106-196` 实现 `Serialize(protocol)` 分发到 `serializeAnthropic` / `serializeOpenAI`，含同角色文本消息合并逻辑。

## T8: 覆盖 Thinking + Reasoning 行为测试
- 影响文件: `internal/llm/thinking_test.go`
- 依赖任务: T4, T5, T7
- 完成标准:
 - `internal/llm/thinking_test.go:45TestSupportsAdaptiveThinking` 验证 4.6/4.7=true、4.5=false、非 Claude=false；
 - `:69TestAnthropicThinkingAdaptive` 断言 4.6 走 adaptive、无 budget_tokens；
 - `:97TestAnthropicThinkingEnabled` 断言非官方模型走 enabled、budget=maxTokens-1；
 - `:130TestAnthropicThinkingDisabled` 断言 thinking=false 时请求体无 thinking 字段；
 - `:154TestAnthropicThinkingBlocksInConversation` 断言 thinking block 的 signature 能往返；
 - `:200`、`:276` 分别覆盖 OpenAI reasoning enabled/disabled。

## T9: 接入主流程
- 影响文件: `internal/tui/tui.go`、`internal/agent/agent.go`、`cmd/mewcode/teammate.go`
- 依赖任务: T1-T7
- 完成标准:
 - `internal/tui/tui.go:352` 用 `llm.NewClient(p, systemPrompt)` 构造 client；
 - `internal/tui/tui.go:360` 把 client 传给 `agent.New(client, m.registry, p.Protocol)`；
 - `internal/agent/agent.go:105` Agent Loop 调用 `a.Client.Stream(ctx, conv, toolSchemas)`；
 - `cmd/mewcode/teammate.go:82` teammate worker 也走 `llm.NewClient(&provider, "")`。

## T10: 端到端验证
- 影响文件: 无（仅运行验证）
- 依赖任务: T9
- 完成标准:
 - `go build ./...` 通过；
 - `go test ./internal/llm/...` 通过（6 个 thinking_test 全绿）；
 - 在 TUI 中发送任意一句话，能看到流式文本（TextDelta）被逐 token 渲染到对话窗口，证明 Stream 通道与事件渲染端到端打通。

## 进度
- [ ] T1
- [ ] T2
- [ ] T3
- [ ] T4
- [ ] T5
- [ ] T6
- [ ] T7
- [ ] T8
- [ ] T9
- [ ] T10