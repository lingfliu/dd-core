# DD Core v1.1 故障注入测试报告

## 1. 测试目标

验证 DD Core 在以下故障场景下的行为：
1. MQ 发布失败
2. Sync 超时
3. Peer 心跳中断（SweepStale）
4. ACL 拒绝
5. Stream 空闲超时

## 2. 测试场景与结果

### 2.1 MQ 发布失败 (FaultInjectionPublishFailure)

**测试**: [dd_data_service_test.go](file:///Users/lingfengliu/git/dd-core/internal/service/dd_data_service_test.go#L97-L115)

**注入方式:** `mock.SetPublishError(context.DeadlineExceeded)`

**预期行为:** `SendAsync` 返回错误，不 panic

**实际结果:** ✅ PASS — 正确返回 publish failure 错误

**恢复行为:** 下一次 Publish 调用恢复正常（MockClient 未持久化错误状态）

---

### 2.2 Sync 超时 (SendSyncTimeout)

**测试**: [dd_data_service_test.go](file:///Users/lingfengliu/git/dd-core/internal/service/dd_data_service_test.go#L80-L95)

**注入方式:** 设置 `TimeoutMs: 10`，不发送响应

**预期行为:** `SendSync` 在 10ms 后返回超时错误 `DD-4081`

**实际结果:** ✅ PASS — 正确返回 `[DD-4081] sync timeout: request_id=req-timeout`

**恢复行为:** 超时后 `pendingRequests` 中该 request_id 被清理，不影响后续请求

**可观测性:**
- `dd_sync_requests_total{status="timeout"}` 计数器 +1
- `dd_timeout_total` 计数器 +1
- slog.Warn 日志输出

---

### 2.3 Peer 心跳中断 (SweepStale)

**测试**: [peer_registry_service_test.go](file:///Users/lingfengliu/git/dd-core/internal/service/peer_registry_service_test.go#L67-L91)

**注入方式:** 注册 peer 后不发送心跳，SweepStale 时间提前 2 秒

**预期行为:** `GetPeers` 返回空列表（stale peer 被过滤）

**实际结果:** ✅ PASS — stale peer 正确过滤

**恢复行为:** 下次 heartbeat 到达后，peer 恢复为 active

**可观测性:**
- `dd_peer_active_count` gauge 下降
- `dd_peer_stale_count` gauge 上升

---

### 2.4 ACL 拒绝 (Deny Precedence)

**测试**: [topic_acl_service_test.go](file:///Users/lingfengliu/git/dd-core/internal/service/topic_acl_service_test.go#L5-L19)

**注入方式:** 设置 deny rule 覆盖 allow rule

**预期行为:** deny 优先于 allow，未授权 pub/sub 被拒绝

**实际结果:** ✅ PASS — deny 规则生效，返回 `DD-4004` ACK deny error

**恢复行为:** 移除 deny rule 后，allow rule 生效

**可观测性:**
- `dd_acl_denied_total` 计数器 +1
- slog.Warn 日志输出

---

### 2.5 Stream 空闲超时 (SweepIdle)

**测试**: [stream_service_test.go](file:///Users/lingfengliu/git/dd-core/internal/service/stream_service_test.go#L87-L110)

**注入方式:** 创建 stream 后不更新 `LastActiveAt`，SweepIdle 触发

**预期行为:** stream session 状态变为 `timeout`

**实际结果:** ✅ PASS — stream 正确标记为 timeout

**恢复行为:** 需要重新 Open stream 建立新会话

---

## 3. 故障恢复矩阵

| 故障类型 | 检测方式 | 恢复方式 | 恢复时间 | 数据丢失 |
|---|---|---|---|---|
| MQ 发布失败 | Publish 返回 error | 调用方重试 | 即时 | 未投递消息 |
| Sync 超时 | Timer 到期 | 自动清理 + 调用方重试 | timeout_ms | 响应丢失 |
| Peer 心跳中断 | SweepStale 扫描 | 心跳恢复后自动 active | lease_ttl | 无（控制面） |
| ACL 拒绝 | Authorize 返回 false | 修改 ACL 规则 | 即时 | 无 |
| Stream 空闲超时 | SweepIdle 扫描 | 重新 Open stream | 即时 | 无 |

## 4. 结论

v1.1 在以下故障场景下行为符合预期：
- MQ 发布失败：优雅降级，返回错误
- Sync 超时：正确超时，资源释放
- Peer 心跳中断：正确标记 stale，查询过滤
- ACL 拒绝：deny 优先，可观测
- Stream 空闲超时：正确标记 timeout

所有故障注入测试通过，恢复行为可控且可观测。
