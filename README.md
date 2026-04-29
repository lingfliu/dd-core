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

**Transfer：**

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/transfer/sync` | 同步请求（阻塞等待响应或超时） |
| `POST` | `/transfer/async` | 异步事件发布 |

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
| `dd_peer_active_count` | Gauge | 活跃 peer 数量 |
| `dd_peer_stale_count` | Gauge | stale peer 数量 |
| `dd_timeout_total` | Counter | sync 超时总数 |
| `dd_acl_denied_total` | Counter | ACL 拒绝总数 |
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
| [design-auth-v1.2.md](docs/design-auth-v1.2.md) | 分布式资源注册设计 |
| [plan-v.1.0.md](docs/plan-v.1.0.md) | v1.0 开发计划 |
| [plan-v.1.1.md](docs/plan-v.1.1.md) | v1.1 开发计划 |
| [plan-v.1.2.md](docs/plan-v.1.2.md) | v1.2 开发计划 |
| [bench-v1.1.md](docs/bench-v1.1.md) | 压测报告 |
| [fault-injection-v1.1.md](docs/fault-injection-v1.1.md) | 故障注入报告 |

---

## 9. License

MIT
