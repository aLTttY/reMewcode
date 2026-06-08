# ch02: 让 AI 开口说话 Spec

## 1. 背景

Agent 落地的第一步是让上层（Agent Loop / TUI / SubAgent）能用同一套接口和 LLM 收发，不必各自面对 SSE 流、Thinking 签名回传、Provider 间消息差异。本章把 LLM 通信、流式响应、Extended Thinking、Token 统计以及两层消息模型封装到 `internal/llm` 与 `internal/conversation`，是 ch03+ 工具循环的前置依赖。

## 2. 目标

交付统一的 `llm.Client` 流式接口和两个内置 Provider 实现（Anthropic、OpenAI Responses），加上 `conversation.Manager` 两层消息模型（内部带 thinking / tool use / tool result 的 `Message`，序列化到具体 Provider 的请求体）。上层（Agent Loop、TUI 装配点、SubAgent、Compact）拿一个 `Client` 就能跑，不再触碰 SSE 细节。

## 3. 功能需求

- F1: `llm.Client` 统一暴露流式接口，输入是会话管理器和工具 schema，输出是事件通道 + 错误通道。
- F2: 客户端工厂按 Provider Protocol 路由到 Anthropic 或 OpenAI 实现，未知 protocol 报错。
- F3: 事件流覆盖五类信号：文本 delta、thinking delta / complete（含签名）、tool call 三段（start / delta / complete）、流结束（含 stop reason 与 usage）、用量统计。所有事件用 sum type 收口。
- F4: Anthropic 客户端基于官方 SDK，支持 Extended Thinking 两种模式：高版本模型走 Adaptive Thinking，低版本回退到固定 budget 的 Enabled Thinking，模型版本能力判断在客户端内部完成。
- F5: OpenAI 客户端基于 Responses API（非 Chat Completions），支持把 reasoning summary 还原成 thinking delta / complete 事件，让上层看到的事件形状和 Anthropic 一致。
- F6: 两个客户端都需要应对 SDK 静默阻塞——通过空闲超时（独立 readNext goroutine + select ctx/idle）兜底，超时归类为网络错误退出。
- F7: 错误分类有 5 类：通用 LLMError、AuthenticationError、RateLimitError（带 RetryAfter）、NetworkError、ContextTooLongError。各客户端把 SDK / HTTP 错误归类到这 5 类之一，上层只面对统一错误。
- F8: `conversation.Message` 支持完整字段：role / content / thinking blocks / tool uses / tool results。所有写操作走 `Manager` 方法，禁止外部直接改 history。
- F9: `Manager` 提供深拷贝读和按 Protocol 序列化两个出口，序列化时不丢字段（thinking signature、tool arguments、tool result IsError 都要原样回到下一轮请求）。
- F10: `Manager` 提供 system-reminder 注入入口，把内容包成 `<system-reminder>` 标签作为 user 消息追加，供 ch04 Plan Mode、ch08 Compact、ch09 Memory 复用。
- F11: 提供模型短名解析器（haiku / sonnet / opus → 具体模型 ID），供 ch13 SubAgent 切模型。

## 4. 非功能需求

- N1: 事件通道有缓冲，SSE 读取与事件分发用独立 channel 解耦，事件写入不阻塞 SSE 读。
- N2: ctx 取消（如 TUI ctrl+c）必须在一个 SSE 事件周期内退出 Stream goroutine，并通过错误通道抛出 NetworkError。
- N3: SDK 静默阻塞要被空闲超时兜底，避免拖死整个 agent loop。
- N4: 序列化层不丢字段（thinking signature / tool arguments / tool result IsError 全部往返保留）。
- N5: `conversation.Manager` 不加锁——单消费者模型，调用方负责串行化（agent loop 单 goroutine 顺序追加）。

## 5. 设计概要

- 核心数据结构:
 - `llm.Client`（流式接口）/ `llm.MaxTokensSetter`（可选接口，让 ch04 升级 max_tokens）
 - `llm.StreamEvent` sum type
 - `llm.UsageInfo`
 - 5 类错误类型
 - `conversation.Message` / `conversation.Manager`（私有 history slice）
- 主流程（每轮 LLM 请求）:
 1. Agent Loop 调 `client.Stream(ctx, conv, toolSchemas)`
 2. 客户端把 Manager 历史序列化成 SDK 入参，调 SDK 流式接口
 3. 独立 goroutine 读 SDK，主 goroutine select ctx / 空闲超时 / SDK 事件
 4. 按 SDK 事件类型 push 对应 `StreamEvent`
 5. 流结束 push `StreamEnd`；异常经错误分类后写到错误通道
- 调用链（模块层级）:
 - TUI 装配 → `llm.NewClient(provider)` → 传给 `agent.New`
 - Agent loop → `Client.Stream` → 消费事件 → 写回 `conversation.Manager`
 - SubAgent / Compact / Teammate worker 复用同一 `Client` 接口
- 与其他模块的交互:
 - 依赖 `internal/config`（Provider 配置、API key、token 上限）
 - 被 `internal/agent`、`internal/agents`、`internal/compact`、`internal/tui`、`cmd/mewcode/teammate` 调用
 - 与 `internal/tools` 解耦：`Stream` 只接 `[]map[string]any` schema，工具注册中心由 `tools.Registry` 提供

## 6. Out of Scope

- 多模态输入（image / PDF）的请求体构造：当前 `Message.Content` 仅 string，未来章节再扩
- 自动重试与指数退避：rate limit 的重试在 ch04 Agent Loop 处理，不在 ch02 范围
- Provider 抽象细分（Bedrock / Vertex / Azure-OpenAI）：当前只支持原生 Anthropic 与原生 OpenAI Responses
- Prompt caching / Cache breakpoints：目标设计已有，本仓库暂未实现

## 7. 完成定义

见 [checklist.md](checklist.md)，所有条目勾上即完成。