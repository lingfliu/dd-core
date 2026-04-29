# DD Core v1.1 开发计划

## 1. 版本目标

v1.1 在 v1.0 已完成的 MQ 调用链路与内存服务发现之上，补齐以下能力：

1. **补课 v1.0 遗留**：完善 topic 常量、补全 HTTP API 层、ACL 集成到数据面、压测/故障注入报告入库；
2. **可编译可运行**：新增 `cmd/main.go` 入口，加载 YAML 配置，启动 MQTT 连接与服务；
3. **HTTP Control API**：暴露 peer/resource/transfer 管理接口；
4. **可观测性基础**：metrics 指标暴露（延迟/吞吐/超时率）+ 结构化日志；
5. **错误码体系**：定义 DD-xxxx 结构化传输错误码；
6. **RegistryStore 抽象**：为 etcd/consul 切换铺路；
7. **Stream 控制面**：stream open/close 生命周期管理（data plane 仍搁置）。

v1.1 交付物为一个可编译、可运行、可通过 HTTP API 管理的 DD Core 单节点二进制。

---

## 2. v1.0 完成度复查

### 2.1 已交付能力

| 模块 | 实现 | 文件 |
|---|---|---|
| DdMessage 统一信封 | mode/protocol/resource/header/payload + Validate | `internal/model/dd_message.go` |
| DdPeerInfo 模型 | id/name/role/status/resources 等 | `internal/model/dd_peer_info.go` |
| Peer 事件模型 | register/heartbeat/resource-report/query | `internal/model/dd_peer_events.go` |
| MQClient 接口 | Publish/Subscribe/Close | `internal/mq/client.go` |
| MQTT provider | eclipse/paho 实现 | `internal/mq/mqtt_client.go` |
| MockClient | 内存模拟 + 故障注入 hook | `internal/mq/mock_client.go` |
| DdDataService | SendSync (correlation+timeout) / SendAsync | `internal/service/dd_data_service.go` |
| PeerRegistryService | register/heartbeat/query via MQ + SweepStale | `internal/service/peer_registry_service.go` |
| TopicAclService | pub/sub deny/allow + MQTT wildcard | `internal/service/topic_acl_service.go` |
| TopicSet | peer register/heartbeat/query/report topics | `internal/service/topics.go` |
| 单元测试 | model 契约、sync/async、timeout、ACL | `*_test.go` |
| 集成测试 | 两 peer + hub + MockClient e2e | `test/integration/dd_peer_e2e_test.go` |

### 2.2 v1.0 遗留项（v1.1 补课）

| # | 遗留项 | 影响 | 优先级 |
|---|---|---|---|
| L1 | TopicSet 不完整，缺 transfer/event topic 常量 | topic 硬编码在测试和服务调用中 | P0 |
| L2 | 无 `cmd/main.go`，无法编译为二进制 | 无法独立运行 | P0 |
| L3 | 无 HTTP API 层 (`api/` 目录不存在) | 无外部可访问的控制面 | P0 |
| L4 | ACL 未集成到 DdDataService/PeerRegistryService | ACL 独立存在，不走数据面 | P0 |
| L5 | 无配置管理（YAML/ENV） | 参数硬编码，无法运维配置 | P1 |
| L6 | 无结构化错误码体系 | 仅 Go error strings，不可观测 | P1 |
| L7 | 压测/故障注入报告未入库 `docs/` | plan v1.0 的 DoD 未完全达标 | P1 |
| L8 | 无 peer unregister 流程 | 只能被动超时剔除 | P2 |
| L9 | peer 状态机缺少 `registering` 中间态 | 只有 active/stale/offline | P2 |

---

## 3. v1.1 新增范围（In Scope）

### 3.1 v1.0 补课
- 补全 TopicSet 常量（transfer/event topic）；
- 新增 `cmd/main.go` 入口 + YAML 配置加载；
- 新增 `internal/config` 配置模块；
- 新增 `api/` HTTP API 层（gin 或 net/http）；
- ACL 集成：在 DdDataService.Publish/Subscribe 前校验、在 PeerRegistryService.handleQuery 前校验；
- 结构化错误码 `internal/model/dd_errors.go`；
- 压测报告 + 故障注入报告入库 `docs/bench-v1.1.md` / `docs/fault-injection-v1.1.md`。

### 3.2 HTTP Control API
基路径：`/dd/api/v1`

**Peer API：**
- `POST /peers/register` — 注册 peer + 返回 lease 元数据
- `POST /peers/{id}/heartbeat` — 续约
- `POST /peers/{id}/unregister` — 主动下线
- `GET /peers/{id}` — 查询单个 peer
- `GET /peers?role=&status=&resource=` — 条件查询

**Transfer API：**
- `POST /transfer/sync` — 同步请求（阻塞等待响应或超时）
- `POST /transfer/async` — 异步事件发布
- `GET /transfer/{requestId}` — 查询传输状态

**Stream API（控制面）：**
- `POST /transfer/stream/open` — 打开流会话
- `POST /transfer/stream/close` — 关闭流会话
- `GET /streams` — 活跃流列表

**Admin API：**
- `GET /health` — 健康检查
- `GET /metrics` — Prometheus 指标端点

### 3.3 可观测性基础
- Prometheus metrics：`dd_sync_requests_total`、`dd_sync_latency_ms`、`dd_async_published_total`、`dd_peer_active_count`、`dd_peer_stale_count`、`dd_timeout_total`、`dd_acl_denied_total`；
- 结构化日志：使用 `slog` 输出 JSON 格式，携带 `request_id`/`correlation_id`/`peer_id`/`topic`。

### 3.4 Stream 控制面
- StreamSession 模型：`stream_id`/`resource`/`source_peer_id`/`target_peer_ids`/`profile`/`status`/`opened_at`/`idle_timeout_ms`；
- open/close 通过 MQ topic `dd/{tenant}/stream/{resource}/open` 和 `dd/{tenant}/stream/{resource}/close` 传递；
- data plane 仅保留接口占位，不做完整实现。

### 3.5 RegistryStore 接口抽象
- 定义 `RegistryStore` 接口（`Get`/`Set`/`Delete`/`List`/`Sweep`）；
- 当前仅提供 `MemoryRegistryStore` 实现；
- 为后续 etcd/consul 切换预留接口。

### 3.6 配置管理
- YAML 配置文件 `config.yaml`；
- 支持 ENV 覆盖；
- 配置项：mqtt broker、tenant、timeout、lease TTL、ACL rules、log level、API listen addr。

---

## 4. v1.1 不做（Out of Scope）

1. Stream data plane（stream open/close 控制面做，data 转发不做）；
2. 持久化存储与恢复（仍依赖 MQ）；
3. 自建重试调度器/DLQ 引擎；
4. 自建 Router 角色；
5. etcd/consul 实际接入（只做 RegistryStore 接口抽象）；
6. MemMQ 接入（prd-memmq-v1.md 为独立组件）；
7. 多节点集群（单节点优先）；
8. 完整的 RBAC/多租户权限模型。

---

## 5. 里程碑与交付物

### M5：v1.0 补课 + 可编译运行（第 1 周）
**交付物：**
1. 补全 TopicSet（transfer/event/stream topic 常量）；
2. `internal/model/dd_errors.go` 错误码体系；
3. `internal/config` 配置模块（YAML + ENV）；
4. `cmd/main.go` 入口：加载配置 → 建 MQTT 连接 → 启动各 service → 启动 HTTP API；
5. ACL 集成：挂载到 DdDataService 和 PeerRegistryService；
6. peer 状态机补 `registering` 中间态 + `POST /peers/{id}/unregister`；
7. `internal/registry/memory_store.go` — MemoryRegistryStore 实现；
8. `internal/registry/store.go` — RegistryStore 接口定义。

**验收：**
- `go build ./cmd/...` 成功；
- 启动后可通过 MQTT broker 完成 peer register/heartbeat/query；
- ACL 拒绝可阻断未授权 pub/sub。

### M6：HTTP API + 可观测性（第 2 周）
**交付物：**
1. `api/` HTTP API 层（Peer API + Transfer API + Admin API）；
2. `internal/observability/metrics.go` — Prometheus 指标注册；
3. `internal/observability/logger.go` — slog 结构化日志封装；
4. 在 DdDataService / PeerRegistryService / AclService 中埋点 metrics 和日志；
5. `internal/observability/middleware.go` — HTTP 请求日志/指标中间件。

**验收：**
- `GET /dd/api/v1/health` 返回 200；
- `POST /dd/api/v1/peers/register` 可注册 peer；
- `POST /dd/api/v1/transfer/sync` 可发起同步请求并返回响应；
- `GET /dd/api/v1/metrics` 输出 Prometheus 指标；
- 所有错误返回结构化 DD-xxxx 错误码。

### M7：Stream 控制面 + 压测/故障注入报告（第 3 周）
**交付物：**
1. `internal/model/dd_stream.go` — StreamSession 模型；
2. `internal/service/stream_service.go` — stream open/close 逻辑；
3. stream open/close topic 订阅与处理；
4. `docs/bench-v1.1.md` — 压测报告（吞吐/延迟/超时率）；
5. `docs/fault-injection-v1.1.md` — 故障注入报告（MQ 中断、心跳中断、消息延迟）。

**验收：**
- stream open 后 session 写入内存注册表；
- stream close 后 session 标记关闭；
- 压测报告包含 p50/p95/p99 指标；
- 故障注入报告描述恢复行为。

---

## 6. 任务拆解（按模块）

### 6.1 `internal/model`
1. 新增 `dd_errors.go`：`DdError` 结构体 + `DD-xxxx` 错误码常量；
2. 新增 `dd_stream.go`：`StreamSession`、`StreamProfile`、`StreamStatus`；
3. 扩展 `DdPeerInfo`：补充 `StatusRegistering` 常量、`Metadata` 字段。

### 6.2 `internal/mq`
无新增。现有 MQClient 接口 + MQTT/Mock 实现已满足需求。

### 6.3 `internal/service`
1. 扩展 `topics.go`：补全 transfer/event/stream topic 常量；
2. 扩展 `DdDataService`：集成 ACL 校验（构造时注入 `*TopicAclService`）；
3. 扩展 `PeerRegistryService`：
   - 集成 ACL 校验；
   - 使用 `RegistryStore` 接口替代直接 `map[string]DdPeerInfo`；
   - 新增 `handleUnregister` 处理 peer 主动下线；
4. 新增 `stream_service.go`：`StreamService`（open/close + 会话管理）；
5. 在 `DdDataService` 和 `PeerRegistryService` 中埋 metrics 和日志。

### 6.4 `internal/registry`（新增）
1. `store.go`：`RegistryStore` 接口（Get/Set/Delete/List/Sweep）；
2. `memory_store.go`：`MemoryRegistryStore` 实现（替换现有 map）。

### 6.5 `internal/config`（新增）
1. `config.go`：Config 结构体 + YAML 加载 + ENV 覆盖；
2. 默认配置文件模板。

### 6.6 `internal/observability`（新增）
1. `metrics.go`：Prometheus 指标定义与注册；
2. `logger.go`：slog 封装（JSON 输出 + request_id 注入）；
3. `middleware.go`：HTTP 中间件（请求日志 + 请求计数 + 延迟直方图）。

### 6.7 `api`（新增）
1. `api/router.go`：路由注册（gin 或 net/http）；
2. `api/handler/peer.go`：Peer API handler；
3. `api/handler/transfer.go`：Transfer API handler；
4. `api/handler/stream.go`：Stream API handler；
5. `api/handler/admin.go`：Health + Metrics handler；
6. `api/middleware/`：recovery、request_id 注入、ACL auth。

### 6.8 `cmd`
1. `cmd/main.go`：入口，加载配置 → 初始化 MQTT → 初始化 services → 启动 HTTP API → 等待信号。

### 6.9 `tests`
1. 单元测试：错误码、RegistryStore、StreamService、配置加载；
2. 集成测试：HTTP API + MQTT（可选 mock）全链路；
3. 压测脚本：`scripts/bench.sh` 使用 `go test -bench` 或 vegeta/wrk；
4. 故障注入脚本：`scripts/fault-inject.sh`。

---

## 7. 统一验收标准（Definition of Done）

1. `go build ./cmd/...` 通过并生成可执行二进制；
2. sync/async 请求可通过 HTTP API 发起并通过 MQ 完成；
3. peer register/heartbeat/query/unregister 全部通过 HTTP API + MQ 联动；
4. topic ACL 在 DdDataService.Publish/Subscribe 前生效；
5. `GET /dd/api/v1/metrics` 返回有效 Prometheus 指标；
6. 错误响应统一返回 `{"code":"DD-xxxx","message":"..."}` 格式；
7. RegistryStore 接口定义清晰，MemoryRegistryStore 通过单测；
8. stream open/close 通过 MQ 完成会话生命周期管理；
9. `docs/bench-v1.1.md` 和 `docs/fault-injection-v1.1.md` 入库；
10. 配置文件 `config.yaml` 模板存在且被 `cmd/main.go` 加载。

---

## 8. 风险与对策

1. **MQTT broker 环境依赖**  
   对策：MockClient 已完备，单元/集成测试不依赖外部 broker；压测可选用公共 broker 或 docker-compose。
2. **ACL 规则复杂度随集成上升**  
   对策：v1.1 仅支持 pattern + action，不引入 role-based 或层级策略。
3. **HTTP API 并发安全**  
   对策：DdDataService 已有 `pendingMu`，PeerRegistryService 已有 `mu`，API handler 复用现有锁保护。
4. **配置热更新需求**  
   对策：v1.1 仅启动时加载，不实现热更新（后续版本考虑）。
5. **Prometheus 指标 cardinality**  
   对策：只按 `mode`/`resource`/`status` 维度聚合，不按 `request_id` 等无限值拆分。

---

## 9. 配置模板（目标形态）

```yaml
# config.yaml
server:
  listen_addr: ":8080"

mq:
  provider: "mqtt"
  mqtt:
    broker_url: "tcp://localhost:1883"
    client_id: "dd-core-hub"
    username: ""
    password: ""

tenant: "default"

sync:
  default_timeout_ms: 3000

discovery:
  heartbeat_interval_sec: 10
  lease_ttl_sec: 30

acl:
  rules:
    - peer_id: "*"
      action: "pub"
      pattern: "dd/default/transfer/#"
      allow: true
    - peer_id: "*"
      action: "sub"
      pattern: "dd/default/transfer/#"
      allow: true

logging:
  level: "info"
  format: "json"
```

---

## 10. 下一步启动开发任务 Prompt

将下面 Prompt 直接发给编码代理启动 v1.1 开发：

```text
你现在是 dd-core 的实现代理。请严格按照 docs/plan-v.1.1.md 执行 v1.1 第一阶段开发（M5：v1.0 补课 + 可编译运行），并在每一步给出变更文件与验证结果。

目标：
1) 补全 TopicSet 常量（transfer/event/stream topic）
2) 新增 internal/model/dd_errors.go 错误码体系
3) 新增 internal/config 配置模块（YAML + ENV）
4) 新增 cmd/main.go 入口
5) ACL 集成到 DdDataService 和 PeerRegistryService
6) peer 状态机补 registering 中间态 + unregister
7) 新增 internal/registry/store.go（接口）+ memory_store.go（实现）

硬性约束：
- 不破坏现有测试（go test ./... 通过）
- topic 使用 MQTT URL 风格：dd/{tenant}/...
- 不实现持久化、重试调度器、DLQ 引擎
- 不引入 router 角色
- 配置使用 YAML + ENV 覆盖，不硬编码

建议落地步骤：
A. 补全 TopicSet（topics.go）
B. 新增 dd_errors.go
C. 新增 config 模块 + config.yaml 模板
D. 新增 registry 接口 + 实现
E. 重构 PeerRegistryService 使用 RegistryStore
F. ACL 集成到 DdDataService/PeerRegistryService
G. 新增 cmd/main.go
H. 运行全部测试验证

输出要求：
1) 列出修改文件
2) 列出运行的测试命令与结果
3) 如果存在待决策项，最后单独列出
```
