# DD Core

基于 MQTT 的去中心化数据分发中间件，支持跨协议桥接（HTTP/CoAP/MQTT）、点对点同步/异步调用、分布式 peer 注册与发现、ACL 授权和 Stream 控制面。

## 1. 核心能力

### 1.1 协议桥接 (Protocol Bridge)

请求入口通过 MQ topic 进入，由对应协议的 bridge 转译到下游真实服务，响应沿原路返回。

```
Ingress (MQ Topic)  ──→  Bridge (HttpBridge / CoapBridge / MqttBridge)  ──→  Egress (Real Service)
```

| Bridge | 协议 | 说明 |
|---|---|---|
| `HttpBridge` | HTTP | 将 DdMessage 还原为 HTTP 请求，调用下游服务后回包 |
| `CoapBridge` | CoAP (UDP) | 原生 CoAP CON 消息构造，支持 GET/POST/PUT/DELETE |
| `MqttBridge` | MQTT | topic 前缀映射转发，如 `dd/tenant-a/event/#` → `dd/tenant-b/event/#` |

### 1.2 调用模式

| 模式 | 机制 | 说明 |
|---|---|---|
| **sync** | Request Topic + Reply Topic + CorrelationId + Timeout | 在超时窗口内等待响应，支持 request_id 去重 |
| **async** | Event Topic + Consumer Group | 发布即忘，重试/DLQ 复用 MQ 原生能力 |
| **stream** | Open → Data → Close | 控制面通过 MQ 管理 stream 会话生命周期（data plane 占位中） |

v1.3 增量：

- `DdDataService` 提供 `SendSyncAsync`（兼容保留 `SendSync`）。
- MQTT 发布按 topic 启用 QoS 分层（关键 sync=QoS1，事件/心跳类=QoS0）。

### 1.5 DdMessage 兼容演进（v1.3）

`DdMessage` 兼容旧字段与新字段：

- 旧：`header` + `headers`
- 新：`version` + `meta` + `protocol_meta`

链路会自动 Normalize：

1. `meta <-> header` 双向补齐；
2. `protocol_meta[protocol] <-> headers` 双向补齐；
3. 未显式设置版本时默认 `version=v1`。

v1.3 还支持全信封 gzip 编解码：

- 发送端按 payload 阈值自动压缩；
- 接收端自动识别并解压，兼容老消息。

`trace_id` 支持入口注入与透传：

- 请求体可传 `trace_id`；
- 或 HTTP 头 `X-Trace-Id` 注入；
- Bridge 响应透传 `trace_id`。

### 1.3 分布式 Peer 注册与发现

```
Hub 定期广播入口 topic  ──→  Edge 订阅发现 Hub
                                  │
                                  ├─→ Auth Peer 鉴权（key/secret）
                                  │
                                  ├─→ Hub 注册资源
                                  │
                                  └─→ ACL 驱动的 peer 列表查询（分页）
```

- **Hub 广播**：`role=hub` 的 peer 每 10s 通过 `dd/{tenant}/peer/hub/broadcast` 发布入口 topic
- **Auth 鉴权**：`role=auth` 的 peer 校验 key/secret 后签发 auth_token
- **ACL 查询**：peer 通过 `dd/{tenant}/peer/list/query` 发起分页查询，hub 按 ACL 规则过滤后返回

### 1.4 安全与 ACL

- **Topic ACL**：按 `peer_id + action(pub/sub) + topic pattern` 判定，deny 优先于 allow
- **分布式 ACL 同步**：hub 通过 MQ broadcast 全量规则（version 号机制），各 peer 本地热更新
- **鉴权**：Auth Peer 签发 HMAC-SHA256 auth_token，hub 验证后写入 RegistryStore

---

## 2. 架构

```
┌──────────────────────────────────────────────────────────┐
│                      CLI / Config                         │
│         (CLI flags > ENV > config.{yaml,json} > defaults) │
├──────────────────────────────────────────────────────────┤
│                      HTTP API                             │
│   /dd/api/v1 ── health, peers, transfer, streams, metrics │
├──────────────────────────────────────────────────────────┤
│                    Core Services                          │
│  ┌──────────────┐ ┌────────────┐ ┌───────────────────┐   │
│  │ DdDataService │ │PeerRegistry│ │ HubDiscovery       │   │
│  │ (sync/async)  │ │ (register) │ │ (broadcast + find) │   │
│  ├──────────────┤ ├────────────┤ ├───────────────────┤   │
│  │ StreamService │ │PeerListSvc │ │ AuthPeerService    │   │
│  │ (open/close)  │ │ (ACL query)│ │ (verify + token)   │   │
│  └──────────────┘ └────────────┘ └───────────────────┘   │
├──────────────────────────────────────────────────────────┤
│                   Protocol Bridges                        │
│       HttpBridge  │  CoapBridge  │  MqttBridge            │
├──────────────────────────────────────────────────────────┤
│                      Observability                        │
│           Prometheus metrics + slog JSON logging          │
└──────────────────────────────────────────────────────────┘
                            │
                    ┌───────┴───────┐
                    │  MQTT Broker   │
                    │ (eclipse/paho) │
                    └───────────────┘
```

---

## 3. 项目结构

```
dd-core/
├── cmd/
│   └── main.go                      # 入口
├── api/
│   ├── router.go                    # HTTP 路由注册
│   └── handler.go                   # handler 实现
├── internal/
│   ├── adapter/
│   │   ├── bridge.go                # ProtocolBridge 接口
│   │   ├── http_bridge.go           # HTTP 协议桥接
│   │   ├── coap_bridge.go           # CoAP 协议桥接（原生 UDP）
│   │   └── mqtt_bridge.go           # MQTT topic 映射转发
│   ├── config/
│   │   ├── config.go                # 配置加载 (YAML/JSON + ENV + CLI)
│   │   ├── cli.go                   # CLI args 解析
│   │   └── config_test.go           # 配置测试
│   ├── model/
│   │   ├── dd_message.go            # DdMessage/DdHeader/DdProtocol
│   │   ├── dd_peer_info.go          # DdPeerInfo（含状态常量）
│   │   ├── dd_peer_events.go        # 注册/心跳/查询事件
│   │   ├── dd_stream.go             # StreamSession 模型
│   │   ├── dd_errors.go             # DD-xxxx 错误码体系
│   │   └── dd_resource_events.go    # Hub广播/Auth鉴权/查询 模型
│   ├── mq/
│   │   ├── client.go                # MQ Client 接口
│   │   ├── mqtt_client.go           # MQTT (paho) 实现
│   │   └── mock_client.go           # 内存 mock（含 MQTT wildcard）
│   ├── observability/
│   │   ├── metrics.go               # Prometheus 指标
│   │   └── middleware.go             # HTTP 请求日志/指标中间件
│   ├── registry/
│   │   ├── memory_store.go          # RegistryStore 接口 + 内存实现
│   │   └── memory_store_test.go
│   └── service/
│       ├── dd_data_service.go       # SendSync/SendAsync (correlation + timeout)
│       ├── peer_registry_service.go # Register/Heartbeat/Query via MQ
│       ├── stream_service.go        # Stream open/close 控制面
│       ├── topic_acl_service.go     # ACL pub/sub 校验
│       ├── auth_peer_service.go     # Auth Peer 鉴权（key/secret → token）
│       ├── hub_discovery.go         # Hub 广播 + Edge 发现
│       ├── peer_list_service.go     # ACL 过滤 + 分页 peer 查询
│       ├── topics.go                # MQTT topic 常量
│       └── *_test.go                # 测试文件（~23 个）
├── test/
│   ├── integration/
│   │   └── dd_peer_e2e_test.go      # 端到端集成测试
│   └── testutil/
│       └── logger.go
├── docs/
│   ├── design-v1.0.md               # 技术方案设计
│   ├── plan-v.1.0.md                # v1.0 开发计划
│   ├── plan-v.1.1.md                # v1.1 开发计划
│   ├── plan-v.1.2.md                # v1.2 开发计划
│   ├── design-auth-v1.2.md          # 分布式资源注册设计
│   ├── bench-v1.1.md                # 压测报告
│   └── fault-injection-v1.1.md      # 故障注入报告
├── config.yaml                      # 默认配置文件
├── go.mod
├── go.sum
└── README.md
```

---

## 4. 依赖环境

| 依赖 | 版本 | 用途 |
|---|---|---|
| Go | ≥1.24.0 | 编译运行 |
| MQTT Broker | 任意兼容 broker（如 Eclipse Mosquitto） | 消息通道 |
| `github.com/eclipse/paho.mqtt.golang` | v1.5.1 | MQTT 客户端 |
| `github.com/prometheus/client_golang` | v1.23.2 | Prometheus 指标暴露 |
| `gopkg.in/yaml.v3` | v3.0.1 | YAML 配置解析 |

**可选依赖（生产建议）：**

| 依赖 | 用途 |
|---|---|
| Docker + Docker Compose | 一键启动 MQTT broker + dd-core |
| Prometheus + Grafana | 指标采集与可视化 |
| Eclipse Mosquitto | 推荐 MQTT broker |

---

## 5. 快速开始

### 5.1 安装

```bash
git clone <repo-url>
cd dd-core
go build -o dd-core ./cmd/main.go
```

### 5.2 启动

**前置条件：** 需要运行一个 MQTT broker（如 Mosquitto）。

```bash
# 方式 1：使用 docker 启动 broker（推荐）
docker run -d --name mosquitto -p 1883:1883 eclipse-mosquitto

# 方式 2：使用本地安装的 mosquitto
brew install mosquitto && mosquitto -d
```

**启动 dd-core：**

```bash
# 最小启动（使用默认 config.yaml）
./dd-core

# 指定配置文件
./dd-core --config=config.yaml

# JSON 配置文件
./dd-core --config=config.json

# CLI 覆盖配置
./dd-core \
  --role=hub \
  --peer-id=hub-01 \
  --peer-name="DD Hub" \
  --tenant=default \
  --listen-addr=:9090 \
  --mqtt-broker=tcp://localhost:1883 \
  --log-level=info \
  --log-format=json

# 内联 JSON 配置覆盖文件
./dd-core --config=config.yaml --config-json='{"tenant":"production"}'
```

**配置加载优先级：** CLI flags > ENV > config file > defaults

### 5.3 环境变量

| 变量 | 说明 |
|---|---|
| `DD_SERVER_LISTEN_ADDR` | HTTP API 监听地址 |
| `DD_PEER_ID` | Peer 标识 |
| `DD_PEER_NAME` | Peer 名称 |
| `DD_PEER_ROLE` | Peer 角色 |
| `DD_TENANT` | 租户名 |
| `DD_MQTT_BROKER_URL` | MQTT broker 地址 |
| `DD_MQTT_CLIENT_ID` | MQTT client id |
| `DD_MQTT_TLS_ENABLED` | 是否启用 MQTT TLS（true/false） |
| `DD_MQTT_TLS_CA_CERT_FILE` | MQTT TLS CA 证书路径 |
| `DD_MQTT_TLS_CLIENT_CERT_FILE` | MQTT mTLS 客户端证书路径 |
| `DD_MQTT_TLS_CLIENT_KEY_FILE` | MQTT mTLS 客户端私钥路径 |
| `DD_MQTT_TLS_INSECURE_SKIP_VERIFY` | 跳过服务端证书校验（仅测试环境） |
| `DD_MQTT_TOPIC_ALIAS_ENABLED` | 启用 MQTT 应用层 TopicAlias PoC（仅 transfer 精确 topic） |
| `DD_SYNC_DEFAULT_TIMEOUT_MS` | sync 默认超时 |
| `DD_DISCOVERY_LEASE_TTL_SEC` | Peer lease TTL |
| `DD_LOG_LEVEL` | 日志级别 (debug/info/warn/error) |
| `DD_LOG_FORMAT` | 日志格式 (json/text) |

### 5.4 API 端点

基路径：`http://localhost:8080/dd/api/v1`

**健康检查：**

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/health` | 健康检查，返回 uptime |
| `GET` | `/metrics` | JSON 格式指标摘要 |

**Peer 管理：**

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/peers/register` | 注册 peer |
| `POST` | `/peers/{id}/heartbeat` | 发送心跳 |
| `POST` | `/peers/{id}/unregister` | 主动注销 |
| `GET` | `/peers/{id}` | 查询单个 peer |
| `GET` | `/peers?role=&status=&resource=` | 条件查询 peer 列表 |
| `GET` | `/resources/{resource}/meta-schema` | 查询资源声明式 meta schema |

**Transfer：**

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/transfer/sync` | 同步请求（阻塞等待响应或超时） |
| `POST` | `/transfer/async` | 异步事件发布 |
| `POST` | `/transfer/async/batch` | 批量异步事件发布（PoC） |
| `GET` | `/transfer/{requestId}` | 查询 transfer 状态 |

`transfer/sync|async` 响应会包含 `route_mode`（`broker`/`direct`），用于观察路由决策结果。

`transfer/sync` 在 v1.3 默认设置：

- `reply_to=dd/{tenant}/transfer/response/{request_id}`（专属响应 topic）；
- 运行期同时兼容监听 `dd/{tenant}/transfer/+/response` 与 `dd/{tenant}/transfer/response/+` 两类响应流。

v1.3 当前 direct 路由行为：

- 当 `target_peer_id` 命中本节点且协议为 HTTP/CoAP 时，API 会优先 direct 旁路到本地配置的 bridge target；
- direct 调用失败时自动回退到 broker 路径；
- 当前 direct 主要覆盖同节点场景，跨节点直连策略在后续迭代完善。

Topic Alias（v1.3）说明：

- 已提供应用层 PoC（`DD_MQTT_TOPIC_ALIAS_ENABLED=true`）；
- 当前只对 `dd/{tenant}/transfer/...` 的精确 topic 做确定性别名映射；
- wildcard topic 与非 transfer topic 不做映射，保证现有桥接兼容；
- Broker 原生 MQTT 5.0 Topic Alias 仍建议在基础设施侧升级后启用。

发布范围说明（v1.3）：

- 已交付：同节点 direct（HTTP/CoAP）、全信封压缩、schema 聚合校验、协议资源运行期统计、批量异步 PoC。  
- 后续增强：Broker 原生 MQTT5 Topic Alias、跨节点 direct 策略、CoAP 长连接池化深化。

CoAP 性能增强（v1.3）：

- CoAP bridge 采用 worker pool + 有界队列处理请求，降低串行阻塞风险；
- 队列满载时写入 DLQ 并记录 `dd_bridge_requests_total{protocol="coap",status="queue_full"}`。
- 增加 CoAP payload 安全阈值保护，超限返回 `coap_payload_too_large`，避免 UDP MTU 截断风险。

服务层并发优化（v1.3）：

- `DdDataService` 的 pending/status/idempotent 查询路径已改为 `RWMutex` 读锁，降低高并发查询场景下的锁竞争。

`transfer/sync|async` v1.3 支持可选字段：

- `trace_id`
- `headers`（旧协议头兼容）
- `protocol_meta`（新协议字段命名空间）

当资源 provider 声明 `resource_meta_schema_v1` 且存在 schema 时，入口会按 required/constraints 做校验。

当同一资源存在多个 provider schema 时，Hub 会聚合 schema：

- `required/optional/protocol_meta_required` 按并集合并；
- `constraints.min/max` 取更严格边界（`min` 取更大，`max` 取更小）；
- `constraints.enum` 取交集。

**Protocol Resources（v1.3）：**

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/protocol-resources/http` | 查询 HTTP 资源视图 |
| `GET` | `/protocol-resources/http/{resource}` | 查询单个 HTTP 资源 |
| `GET` | `/protocol-resources/mqtt/topics` | 查询 MQTT topic 资源（支持 `direction/peer_id/qos`） |
| `GET` | `/protocol-resources/mqtt/topics/{name}` | 查询指定 MQTT topic |
| `GET` | `/protocol-resources/coap` | 查询 CoAP 资源视图 |
| `GET` | `/protocol-resources/coap/{resource}` | 查询单个 CoAP 资源 |

协议资源接口返回中会包含 `request_meta_schema`（若该资源存在声明式 schema）。
并包含 `runtime_stats`（如 `query_count`、`last_queried_at`）用于运行期观测。

Bridge 失败消息会发布到 `dd/{tenant}/dlq/bridge`（包含原因、topic、payload base64）。

**Stream：**

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/transfer/stream/open` | 打开 stream 会话 |
| `POST` | `/transfer/stream/close` | 关闭 stream 会话 |
| `GET` | `/streams` | 活跃 stream 列表 |

**Prometheus：**

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/metrics` | Prometheus 格式指标 |

### 5.5 示例调用

```bash
# 注册 peer
curl -X POST http://localhost:8080/dd/api/v1/peers/register \
  -H "Content-Type: application/json" \
  -d '{"id":"edge-01","name":"Edge Node 1","role":"edge"}'

# 发送心跳
curl -X POST http://localhost:8080/dd/api/v1/peers/edge-01/heartbeat

# 查询所有 peer
curl http://localhost:8080/dd/api/v1/peers

# 查询指定资源 provider
curl "http://localhost:8080/dd/api/v1/peers?resource=order.query"

# 同步调用
curl -X POST http://localhost:8080/dd/api/v1/transfer/sync \
  -H "Content-Type: application/json" \
  -d '{"resource":"order.query","protocol":"http","source_peer_id":"edge-01","target_peer_id":"edge-02","timeout_ms":3000}'

# 异步事件
curl -X POST http://localhost:8080/dd/api/v1/transfer/async \
  -H "Content-Type: application/json" \
  -d '{"resource":"sensor.temp","protocol":"mq","source_peer_id":"edge-01"}'

# 批量异步事件（PoC）
curl -X POST http://localhost:8080/dd/api/v1/transfer/async/batch \
  -H "Content-Type: application/json" \
  -d '{"items":[{"resource":"sensor.temp","protocol":"mq","source_peer_id":"edge-01","payload":"eyJ2IjoxfQ=="},{"resource":"sensor.humidity","protocol":"mq","source_peer_id":"edge-01","payload":"eyJ2IjoyfQ=="}]}'

# 查询协议化 HTTP 资源
curl http://localhost:8080/dd/api/v1/protocol-resources/http

# 查询 MQTT topic 资源（仅发布方向）
curl "http://localhost:8080/dd/api/v1/protocol-resources/mqtt/topics?direction=pub"

# 查询资源 meta schema
curl http://localhost:8080/dd/api/v1/resources/sensor.temp/meta-schema

# 同节点 HTTP direct 旁路（route_mode=direct）
curl -X POST http://localhost:8080/dd/api/v1/transfer/sync \
  -H "Content-Type: application/json" \
  -d '{"resource":"edge-a","protocol":"http","source_peer_id":"client-a","target_peer_id":"edge-a","protocol_meta":{"http":{"method":"GET","path":"/health"}}}'

# 同节点 CoAP direct 旁路（route_mode=direct）
curl -X POST http://localhost:8080/dd/api/v1/transfer/sync \
  -H "Content-Type: application/json" \
  -d '{"resource":"edge-a","protocol":"coap","source_peer_id":"client-a","target_peer_id":"edge-a","protocol_meta":{"coap":{"method":"GET","path":"/status"}}}'

# 健康检查
curl http://localhost:8080/dd/api/v1/health

# 指标
curl http://localhost:8080/dd/api/v1/metrics
```

---

## 6. Prometheus 指标

| 指标 | 类型 | 说明 |
|---|---|---|
| `dd_sync_requests_total{resource,status}` | Counter | sync 请求按 resource 和状态计数 |
| `dd_sync_latency_ms{resource}` | Histogram | sync 请求延迟分布 |
| `dd_async_published_total{resource,status}` | Counter | async 发布按 resource 和状态计数 |
| `dd_async_batch_requests_total{status}` | Counter | async batch 请求计数（ok/partial/error） |
| `dd_peer_active_count` | Gauge | 活跃 peer 数量 |
| `dd_peer_stale_count` | Gauge | stale peer 数量 |
| `dd_timeout_total` | Counter | sync 超时总数 |
| `dd_acl_denied_total` | Counter | ACL 拒绝总数 |
| `dd_bridge_requests_total{protocol,mode,status}` | Counter | bridge 请求总数（ok/error/invalid/publish_error） |
| `dd_bridge_latency_ms{protocol,mode}` | Histogram | bridge 请求时延分布 |
| `dd_bridge_retries_total{protocol}` | Counter | bridge 重试次数 |
| `dd_resource_meta_validate_total{resource,result}` | Counter | 资源 meta schema 校验次数 |
| `dd_route_decision_total{mode}` | Counter | 路由决策统计（broker/direct） |
| `dd_protocol_resource_query_total{protocol,mode}` | Counter | 协议资源 API 查询次数（list/detail） |
| `dd_http_requests_total{method,path,status}` | Counter | HTTP 请求计数 |
| `dd_http_request_duration_ms{method,path}` | Histogram | HTTP 请求延迟分布 |

---

## 7. 运行测试

```bash
# 运行所有测试
go test ./... -count=1

# 带详细输出
go test ./... -v -count=1

# 仅单元测试
go test ./internal/... -count=1

# 运行集成测试
go test ./test/integration/ -v -count=1

# 运行基准测试
go test -bench=. -benchmem ./internal/service/

# 测试覆盖率
go test ./... -cover
```

---

## 8. 文档

| 文档 | 说明 |
|---|---|
| [design-v1.0.md](docs/design-v1.0.md) | 技术方案设计（MQ 原生分发版） |
| [design-v.1.3.md](docs/design-v.1.3.md) | v1.3 传输优化与协议演进设计 |
| [analysis-bridge-protocol-v1.md](docs/analysis-bridge-protocol-v1.md) | 协议缺陷与性能瓶颈分析 |
| [design-auth-v1.2.md](docs/design-auth-v1.2.md) | 分布式资源注册设计 |
| [plan-v.1.0.md](docs/plan-v.1.0.md) | v1.0 开发计划 |
| [plan-v.1.1.md](docs/plan-v.1.1.md) | v1.1 开发计划 |
| [plan-v.1.2.md](docs/plan-v.1.2.md) | v1.2 开发计划 |
| [plan-v.1.3.md](docs/plan-v.1.3.md) | v1.3 开发计划、覆盖状态与 §11 最终交付摘要 |
| [bench-v1.1.md](docs/bench-v1.1.md) | 压测报告 |
| [fault-injection-v1.1.md](docs/fault-injection-v1.1.md) | 故障注入报告 |

---

## 9. License

MIT
