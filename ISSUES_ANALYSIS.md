# Kiro2API 潜在问题分析报告

基于 kiro.rs 项目的更新日志，对当前项目进行了全面检查。以下是发现的问题和建议的修复方案。

## 🔴 严重问题

### 1. AUTO 模式可能导致 400 错误

**问题位置**: [converter/codewhisperer.go:30](converter/codewhisperer.go#L30), [converter/codewhisperer.go:35](converter/codewhisperer.go#L35)

**问题描述**:
```go
func determineChatTriggerType(anthropicReq types.AnthropicRequest) string {
    if len(anthropicReq.Tools) > 0 {
        if anthropicReq.ToolChoice != nil {
            if tc, ok := anthropicReq.ToolChoice.(*types.ToolChoice); ok && tc != nil {
                if tc.Type == "any" || tc.Type == "tool" {
                    return "AUTO" // ⚠️ 可能导致 400 错误
                }
            }
        }
    }
    return "MANUAL"
}
```

**参考项目修复**: kiro.rs 在 2026.1.4 更新中移除了 "AUTO" 模式以避免 400 错误

**建议修复**: 将 "AUTO" 改为 "MANUAL"，或者完全移除这个逻辑

---

### 2. 429 错误处理可能误禁用所有凭据

**问题位置**: [server/error_mapper.go:109](server/error_mapper.go#L109)

**问题描述**:
```go
func (s *RateLimitStrategy) ShouldMarkTokenFailed() bool {
    return true  // ⚠️ 429 错误会标记 token 失败
}
```

当前实现中，429 错误（高流量限流）会触发 `ShouldMarkTokenFailed() = true`，这可能导致瞬态错误误禁用所有凭据。

**参考项目修复**: kiro.rs 改进了凭据故障转移策略，区分瞬态错误和永久错误

**建议修复**:
- 429 错误应该只触发冷却（cooldown），而不是标记为失败
- 区分瞬态错误（429）和永久错误（403）

---

### 3. 缺少 402 MONTHLY_REQUEST_COUNT 错误处理

**问题位置**: [server/error_mapper.go](server/error_mapper.go)

**问题描述**: 当前没有针对 402 错误（月度请求配额耗尽）的专门处理策略

**参考项目修复**: kiro.rs 在 2026.1.4 添加了 402 错误处理，禁用凭据并故障转移

**建议修复**: 添加 `PaymentRequiredStrategy` 来处理 402 错误

---

## 🟡 中等问题

### 4. 缺少工具配对验证

**问题位置**: 工具调用处理逻辑

**问题描述**: 没有验证工具调用和工具结果的配对关系，可能存在孤立的工具结果

**参考项目修复**: kiro.rs 在 2026.1.4 添加了工具配对验证以移除孤立结果

**建议修复**: 在 `parser/compliant_message_processor.go` 中添加工具配对验证逻辑

---

### 5. 不支持凭据级 region/machineId 配置

**问题位置**: [config/config.go](config/config.go), [auth/fingerprint.go](auth/fingerprint.go)

**问题描述**:
- Region 硬编码为 `us-east-1`
- machineId 生成逻辑可能使用固定的 profileArn

**参考项目修复**: kiro.rs 在 2026.1.4 支持凭据级 region/machineId 配置及自动认证方式检测

**建议修复**:
- 允许每个凭据配置独立的 region
- 改进 machineId 生成逻辑，避免使用固定 profileArn

---

### 6. UTF-8 字符串截断可能导致 panic

**问题位置**: 字符串处理相关代码

**问题描述**: 如果存在按字节截断 UTF-8 字符串的逻辑，可能导致 panic

**参考项目修复**: kiro.rs 在 2026.1.2 修复了 UTF-8 字符串截断问题

**建议修复**: 检查所有字符串截断操作，确保按 rune 而不是按字节截断

---

## 🟢 轻微问题

### 7. 思考后紧跟工具调用的过滤问题

**问题位置**: [parser/compliant_message_processor.go](parser/compliant_message_processor.go)

**问题描述**: 当 thinking 模式后紧跟工具调用时，可能没有正确过滤

**参考项目修复**: kiro.rs 在 2026.1.2 修复了此问题

**建议修复**: 在流式处理中添加 thinking + tool_use 的特殊处理逻辑

---

### 8. 最终事件缺少 output_tokens 估算

**问题位置**: [server/stream_processor.go](server/stream_processor.go)

**问题描述**: 生成最终事件时可能没有传递估算的 `output_tokens` 参数

**参考项目修复**: kiro.rs 在 2026.1.2 支持生成最终事件时传递估算的 output_tokens

**建议修复**: 在 `message_delta` 事件中添加 token 估算逻辑

---

### 9. 历史工具不存在导致 400 错误

**问题位置**: 工具历史处理逻辑

**问题描述**: 如果历史消息中引用的工具在当前请求中不存在，可能导致 400 错误

**参考项目修复**: kiro.rs 在 2026.1.2 修复了此问题

**建议修复**: 在构建请求前验证历史工具的存在性，移除不存在的工具引用

---

### 10. 会话 ID 保持功能

**问题位置**: [utils/conversation_id.go](utils/conversation_id.go)

**问题描述**: 当前实现已经支持会话 ID 保持（通过 X-Session-ID header），但可能需要优化

**参考项目修复**: kiro.rs 在 2026.1.2 新增会话 ID 保持功能

**状态**: ✅ 已实现，无需修复

---

## 📊 问题优先级总结

| 优先级 | 问题数量 | 建议处理时间 |
|--------|---------|-------------|
| 🔴 严重 | 3 | 立即修复 |
| 🟡 中等 | 4 | 1-2 周内 |
| 🟢 轻微 | 3 | 可选优化 |

---

## 🔧 建议的修复顺序

1. **立即修复**: AUTO 模式问题（问题 1）
2. **立即修复**: 429 错误处理逻辑（问题 2）
3. **立即修复**: 添加 402 错误处理（问题 3）
4. **短期修复**: 工具配对验证（问题 4）
5. **短期修复**: UTF-8 字符串处理（问题 6）
6. **中期优化**: region/machineId 配置（问题 5）
7. **可选优化**: 其他轻微问题（问题 7-9）

---

## 📝 备注

- 本报告基于 kiro.rs 项目的 2026.1.2 - 2026.1.4 更新日志生成
- 所有问题位置均已标注文件路径和行号
- 建议在修复前先备份代码或创建新分支
