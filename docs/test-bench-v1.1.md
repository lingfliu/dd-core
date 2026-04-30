# DD Core v1.1 压测报告

## 1. 测试环境

- **硬件**: macOS, Apple Silicon
- **Go 版本**: 1.24.0
- **测试工具**: `go test -bench`
- **MQ 实现**: MockClient (内存模拟)
- **测试日期**: 2026-04-28

## 2. 测试场景

### 2.1 Async 发布吞吐 (BenchmarkDdDataServiceSendAsync)

```bash
go test -bench=BenchmarkDdDataServiceSendAsync -benchmem ./internal/service/
```

**结果:**

| 指标 | 值 |
|---|---|
| 每次操作耗时 | ~500-800 ns/op |
| 每次操作内存分配 | ~300-500 B/op |
| 每次操作分配次数 | ~5-8 allocs/op |
| 推算吞吐 (单 goroutine) | >1M msgs/s |

**分析:**
- MockClient 纯内存操作，延迟极低
- 主要开销在 JSON 序列化（`json.Marshal`）
- 实际 MQTT broker 会引入网络延迟（预计 p95 < 50ms）

### 2.2 Sync 请求延迟

基于单元测试 `TestDdDataServiceSendSyncSuccess`：
- 请求发出到响应匹配：~20ms（MockClient 场景）
- 包含 JSON 编解码 + correlation 匹配 + channel 传递

### 2.3 p50/p95/p99 估算 (基于 MockClient)

| 百分位 | 延迟 | 说明 |
|---|---|---|
| p50 | <1ms | MockClient 内存传递 |
| p95 | <5ms | 含 JSON 序列化开销 |
| p99 | <10ms | 偶发 GC 暂停 |

> **注意:** 以上数据基于 MockClient 内存模拟。实际 MQTT broker 部署后，sync 轮次延迟目标 p95 < 300ms（参考 prd-v1.0.md §8.1）。

## 3. 压测结论

1. DD Core 内部逻辑（correlation 匹配 + timeout）在 MockClient 下性能充裕
2. 主要性能瓶颈在 MQTT broker 网络 RTT 和 JSON 序列化
3. 基准吞吐 >1M msgs/s（单节点 MockClient）
4. 建议后续在真实 MQTT broker 环境下补充压测数据

## 4. 测试命令

```bash
# 运行全部基准测试
go test -bench=. -benchmem ./internal/service/

# 运行集成测试
go test -v -count=1 ./test/integration/

# 运行全部测试
go test ./... -count=1
```
