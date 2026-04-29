# DD Core v1.2 开发计划

## 1. 版本目标

v1.2 在 v1.1 已完成的 MQ 调用链路、HTTP API、可观测性和 Stream 控制面之上，新增三大功能方向：

1. **配置增强**：main.go CLI args 解析 + JSON 配置文件支持 + YAML/JSON 双格式自动识别；
2. **协议桥接器**：HTTP / MQTT / CoAP 三种协议的 Ingress→MQ→Egress 可复用桥接组件；
3. **分布式鉴权设计**：所有状态与资源查询通过 MQTT 完成，peer 间基于 key/secret 的分布式认证与授权机制（独立 Wave 设计先行）。

v1.2 交付物为一个可通过 CLI 灵活配置、支持跨协议数据桥接、具备分布式鉴权设计方案的 DD Core 二进制。

---

## 2. v1.1 完成度复查

| 模块 | 状态 | 文件 |
|---|---|---|
| DdMessage 信封 + sync/async | ✅ | `internal/model/dd_message.go` |
| MQClient 接口 + MQTT provider | ✅ | `internal/mq/` |
| DdDataService (correlation+timeout) | ✅ | `internal/service/dd_data_service.go` |
| PeerRegistryService (via MQ) | ✅ | `internal/service/peer_registry_service.go` |
| TopicAclService (pub/sub) | ✅ | `internal/service/topic_acl_service.go` |
| StreamService (open/close) | ✅ | `internal/service/stream_service.go` |
| HTTP API (peer/transfer/stream) | ✅ | `api/` |
| RegistryStore 接口 + MemoryStore | ✅ | `internal/registry/` |
| 错误码体系 DD-xxxx | ✅ | `internal/model/dd_errors.go` |
| 配置模块 (YAML + ENV) | ✅ | `internal/config/` |
| Prometheus metrics + slog | ✅ | `internal/observability/` |
| cmd/main.go 可编译入口 | ✅ | `cmd/main.go` |
| 压测 + 故障注入报告 | ✅ | `docs/bench-v1.1.md` `docs/fault-injection-v1.1.md` |

### v1.1 功能缺口（v1.2 要补）

| # | 缺口 | 说明 |
|---|---|---|
| G1 | 协议桥接为测试临时代码 | `subscribePeerProtocolBridge` 仅在集成测试中，不可复用 |
| G2 | main.go 仅支持 `-config` 单 flag | 无 `--role`/`--peer-id`/`--tenant` 等 CLI 覆盖 |
| G3 | 配置仅支持 YAML | 无 JSON 配置文件支持 |
| G4 | DdProtocol 有 CoAP 枚举但无实现 | `DdProtocolSocket` 和 CoAP 均无桥接代码 |
| G5 | 无分布式鉴权 | ACL 为本地规则，key/secret 字段闲置 |
| G6 | Hub 资源目录查询仅走本地 store | 无跨 peer MQ 查询机制 |

---

## 3. Wave 1：配置增强 + HTTP 协议桥接

### 3.1 目标

1. main.go 支持完整 CLI args：`--role`、`--peer-id`、`--peer-name`、`--tenant`、`--config`(yaml/json)、`--config-json`(inline JSON string)
2. 配置加载支持 YAML 和 JSON 双格式，按文件扩展名自动识别
3. CLI/ENV/配置文件的优先级规则：**CLI flags > ENV > config file > defaults**
4. HTTP Protocol Bridge 作为可复用独立组件
5. 复用桥接器重构集成测试，消除临时代码

### 3.2 详细范围

#### 3.2.1 CLI args 增强

```text
dd-core \
  --config=config.yaml \           # 配置文件路径（支持 .yaml/.yml/.json）
  --config-json='{"tenant":"x"}' \ # 内联 JSON 配置（与 --config 合并，内联优先级更高）
  --role=hub \                      # peer 角色：edge|term|hub|edge_hub
  --peer-id=hub-01 \                # 本 peer 唯一标识
  --peer-name="DD Hub 01" \         # 本 peer 显示名称
  --tenant=default \                # 租户名（覆盖配置文件）
  --listen-addr=:9090 \             # HTTP API 监听地址
  --mqtt-broker=tcp://broker:1883 \ # MQTT broker 地址
  --log-level=debug \               # 日志级别
  --log-format=text                  # 日志格式：json|text
```

**优先级规则：**
```
CLI flags > ENV (DD_xxx) > config file > defaults
```

**特殊：`--config-json`**
- 与 `--config` 指定的文件内容做 **merge**（内联 JSON 覆盖文件中的同名字段）
- 适用于容器化场景（通过环境变量注入完整配置）

#### 3.2.2 配置模块扩展

**文件：`internal/config/config.go`**

新增：
- `LoadPath(path string)` — 按扩展名自动选择 YAML/JSON parser
- `MergeJSON(base *Config, jsonStr string) error` — 合并内联 JSON
- `LoadWithArgs(args CLIConfig) (*Config, error)` — 统一加载入口
- `CLIConfig` 结构体 — 承载所有 CLI 参数

**修改：**
- `Config` 结构体补充字段：`Peer` 配置块（ID/Name/Role）
- 移除 `LoadOrDefault` 中的环境变量预填充，统一到优先级链

**新增 `internal/config/cli.go`：**
```go
type CLIConfig struct {
    ConfigPath string
    ConfigJSON string
    Role       string
    PeerID     string
    PeerName   string
    Tenant     string
    ListenAddr string
    MQTTBroker string
    LogLevel   string
    LogFormat  string
}
```

#### 3.2.3 HTTP Protocol Bridge

**文件：`internal/adapter/http_bridge.go`（新包 `internal/adapter`）**

```go
type HTTPBridge struct {
    mqClient   mq.Client
    topics     service.TopicSet
    peerID     string
    httpClient *http.Client
    // targetURL 为下游真实 HTTP 服务的 base URL
    targetURL  string
}
```

**功能：**
1. `Start(ctx)` — 订阅 `dd/{tenant}/transfer/{peerID}/request` 和 `dd/{tenant}/event/{peerID}/publish`
2. 收到 sync request 后：
   - 解析 `DdMessage.Headers` 中的 `method`/`path`/`headers`
   - 发起 HTTP 请求到 `targetURL + path`
   - 将 HTTP response body 封装为 `DdMessage` 发布到 `reply_to` topic
3. 收到 async event 后：
   - 同上发起 HTTP 请求（fire-and-forget）
4. CoAP 协议识别：当 `DdMessage.Protocol == "coap"` 时，将请求转发给 CoAP bridge 处理（依赖注入）

**关键设计决策：**
- bridge 不持有 `DdDataService`，而是直接用 `mqClient` 发布响应——解耦数据面与控制面
- `Headers` map 中约定的 key：
  - `method` — HTTP method
  - `path` — URL path（追加到 targetURL）
  - `content-type` — Content-Type header
  - 其余 headers key 直接透传为 HTTP headers

**文件：`internal/adapter/bridge.go`**

```go
// ProtocolBridge 统一桥接器接口
type ProtocolBridge interface {
    // Start 订阅 MQ topic 并开始桥接处理
    Start(ctx context.Context) error
    // Protocol 返回桥接的协议类型
    Protocol() model.DdProtocol
}
```

#### 3.2.4 修改集成测试

将 `subscribePeerProtocolBridge` 替换为 `adapter.HTTPBridge`，验证桥接器可复用性。

### 3.3 交付物清单

| # | 文件 | 类型 |
|---|---|---|
| 1 | `internal/config/config.go` | 修改：JSON 支持 + Config.Peer + LoadPath |
| 2 | `internal/config/cli.go` | 新增：CLIConfig + CLI args 解析 |
| 3 | `internal/config/config_test.go` | 修改：JSON 测试用例 |
| 4 | `internal/config/cli_test.go` | 新增：CLI 优先级测试 |
| 5 | `internal/adapter/bridge.go` | 新增：ProtocolBridge 接口 |
| 6 | `internal/adapter/http_bridge.go` | 新增：HTTP Bridge 实现 |
| 7 | `internal/adapter/http_bridge_test.go` | 新增：HTTP Bridge 单元测试 |
| 8 | `cmd/main.go` | 修改：CLI args + 启动 HTTPBridge |
| 9 | `test/integration/dd_peer_e2e_test.go` | 修改：使用 HTTPBridge 替换临时桥接代码 |
| 10 | `config.yaml` | 修改：补充 peer 配置块 |

### 3.4 验收标准

1. `dd-core --config-json='{"tenant":"test"}' --peer-id=p1 --role=edge` 启动成功，tenant 覆盖为 `test`
2. `dd-core --config=config.json` 加载 JSON 配置文件成功
3. HTTP Bridge 启动后，对 `dd/default/transfer/{peerID}/request` 发 sync 请求，bridge 自动向后端 HTTP 服务转发并回包
4. 集成测试 `TestDdPeerABHubRegistryAndSyncAsyncAccess` 使用 `HTTPBridge` 重构后仍然 PASS
5. CLI flags 优先级：`--tenant=cli` 覆盖 ENV `DD_TENANT=env` 和配置文件 `tenant: file`

---

## 4. Wave 2：MQTT Bridge + CoAP Bridge

### 4.1 目标

1. MQTT Bridge — 跨 topic/跨租户的 MQTT 消息转发桥接
2. CoAP Protocol Bridge — 将 MQ 请求转为 CoAP 协议调用
3. `DdProtocol` 枚举扩展为 `coap`

### 4.2 详细范围

#### 4.2.1 CoAP 协议桥接

**依赖：** `github.com/plgd-dev/go-coap/v3`（或 `github.com/dustin/go-coap`）

**文件：`internal/adapter/coap_bridge.go`**

```go
type CoAPBridge struct {
    mqClient  mq.Client
    topics    service.TopicSet
    peerID    string
    targetURL string  // coap://backend:5683
}
```

**功能：**
1. 订阅 `dd/{tenant}/transfer/{peerID}/request`，筛选 `Protocol == "coap"` 的消息
2. 从 `Headers["path"]` 和 `Headers["method"]` 构建 CoAP 请求
3. CoAP method 映射：`GET`→`coap.GET`, `POST`→`coap.POST`, `PUT`→`coap.PUT`, `DELETE`→`coap.DELETE`
4. 将 CoAP response payload 封装回 `DdMessage` 发布到 `reply_to`

**CoAP 特有处理：**
- CoAP 是 UDP 协议，需设置独立的超时和重试
- `Headers["coap-confirmable"]` — 是否使用 CON 消息（默认 true）
- 不支持大 payload（CoAP 通常 < 1152 bytes），超出截断并记录 warn

#### 4.2.2 MQTT Bridge — Topic 映射转发

**文件：`internal/adapter/mqtt_bridge.go`**

```go
type MQTTBridge struct {
    mqClient mq.Client
    topics   service.TopicSet
    peerID   string
    // TopicMapping: source topic → target topic
    mappings []TopicMapping
}

type TopicMapping struct {
    SourceTopic string
    TargetTopic string
}
```

**功能：**
1. 对每个 `TopicMapping`，订阅 `SourceTopic`
2. 收到消息后原样转发到 `TargetTopic`（保留 payload，不重新编码）
3. 支持 wildcard source topic

**使用场景：**
- 跨租户桥接：`dd/tenant-a/event/#` → `dd/tenant-b/event/#`
- 跨环境桥接：本地 dev MQTT broker ↔ 云端 prod MQTT broker
- 协议无关：payload 保留原始二进制，不做解析

#### 4.2.3 DdProtocol 扩展

**文件：`internal/model/dd_message.go`**

```go
const (
    DdProtocolHTTP   DdProtocol = "http"
    DdProtocolMQ     DdProtocol = "mq"
    DdProtocolSocket DdProtocol = "socket"
    DdProtocolStream DdProtocol = "stream"
    DdProtocolCoAP   DdProtocol = "coap"  // 新增
)
```

同时在 `Validate()` 中添加 CoAP 到合法协议列表。

### 4.3 交付物清单

| # | 文件 | 类型 |
|---|---|---|
| 11 | `internal/adapter/coap_bridge.go` | 新增：CoAP Bridge |
| 12 | `internal/adapter/coap_bridge_test.go` | 新增：CoAP Bridge 测试 |
| 13 | `internal/adapter/mqtt_bridge.go` | 新增：MQTT Bridge（topic 映射） |
| 14 | `internal/adapter/mqtt_bridge_test.go` | 新增：MQTT Bridge 测试 |
| 15 | `internal/model/dd_message.go` | 修改：新增 DdProtocolCoAP |
| 16 | `internal/config/config.go` | 修改：新增 Bridge 配置块 |
| 17 | `cmd/main.go` | 修改：按配置启动各 bridge |
| 18 | `config.yaml` | 修改：bridge 配置示例 |

### 4.4 验收标准

1. CoAP Bridge 可通过 MQ 接收请求，转为 CoAP 调用下游服务，响应回 MQ
2. MQTT Bridge 可将 `dd/tenant-a/event/#` 消息转发到 `dd/tenant-b/event/#`
3. `DdMessage.Protocol == "coap"` 的 message 通过 Validate 校验
4. 所有 bridge 可通过配置文件的 `bridges` 段启用/禁用

---

## 5. Wave 3：分布式鉴权机制设计（Design-Only Wave）

### 5.1 目标

本 Wave **不写实现代码**，仅产出一份完整的分布式鉴权设计文档 `docs/design-auth-v1.2.md`，覆盖：

1. 所有状态查询、资源查询统一走 MQTT（而非 HTTP API 直查本地 store）
2. peer 间基于 key/secret 的分布式认证流程
3. 授权决策的分布式执行机制
4. 凭证轮换与吊销机制
5. 与现有 TopicAclService 的衔接方案

### 5.2 设计原则

1. **无中心化 Auth Server**：每个 peer 独立执行鉴权决策
2. **MQ 作为可信通道**：所有认证消息（challenge/response/token）走 MQ
3. **凭证即资源**：peer 的 key/secret 通过 MQ 的资源上报机制同步
4. **渐进式信任**：peer 注册时仅授信 registering，心跳通过后升至 active
5. **ACL 规则分布式同步**：ACL 规则变更通过 MQ 广播，各 peer 本地缓存

### 5.3 设计文档 Outline

```
docs/design-auth-v1.2.md

1. 分布式鉴权总览
   1.1 为什么不用中心化 Auth Server
   1.2 信任模型的演进路径
   1.3 与 TopicAclService 的关系

2. 认证流程 (Authentication)
   2.1 Peer 注册时的身份声明
   2.2 Challenge-Response 握手协议
   2.3 Session Token 签发与验证
   2.4 Token 格式与过期策略
   2.5 跨 peer 调用的身份传递

3. 授权机制 (Authorization)
   3.1 ACL 规则的 MQ 同步协议
   3.2 规则版本号与冲突解决
   3.3 本地缓存与热更新
   3.4 分布式 deny 优先语义
   3.5 资源级授权 (resource-level ACL)

4. 凭证管理
   4.1 Key/Secret 的生成与分发
   4.2 凭证轮换流程（无中断）
   4.3 凭证吊销与黑名单广播
   4.4 泄露检测与应急吊销

5. 状态查询 MQ 化
   5.1 当前状态查询的路径分析
   5.2 所有查询改为 MQ request/reply 的改造方案
   5.3 查询超时与降级策略
   5.4 Hub 角色的特殊处理

6. Topic 设计
   6.1 dd/{tenant}/peer/auth/challenge
   6.2 dd/{tenant}/peer/auth/response
   6.3 dd/{tenant}/peer/auth/token
   6.4 dd/{tenant}/peer/auth/revoke
   6.5 dd/{tenant}/peer/acl/sync

7. 安全考量
   7.1 重放攻击防护
   7.2 MQ 传输加密（TLS）
   7.3 payload 签名与完整性校验
   7.4 审计日志设计

8. v1.3 实现计划
   8.1 分阶段实现路线图
   8.2 Wave 1：Token 签发与验证
   8.3 Wave 2：ACL 分布式同步
   8.4 Wave 3：凭证轮换与吊销
```

### 5.4 关键设计决策

| 决策项 | 推荐方案 | 备选 |
|---|---|---|
| Token 格式 | JWT（HMAC-SHA256，secret 签名） | 自定义 binary token |
| ACL 同步方式 | MQ broadcast（all peers 订阅） | Hub 单点推送 |
| 凭证存储 | 内存（重启后重新注册获取） | 加密落盘 |
| 吊销传播延迟 | 尽力广播 + TTL 兜底（max 30s） | 强一致性共识 |
| Challenge 算法 | HMAC-SHA256(challenge, secret) | TLS-PSK |

### 5.5 交付物

仅一份文档：`docs/design-auth-v1.2.md`

### 5.6 验收标准

1. 文档完整覆盖 §5.3 的 9 个章节
2. 每个认证/授权流程有 Mermaid 时序图
3. Topic 定义完整，与现有 topic 规范兼容
4. 安全考量覆盖 OWASP Top 10 for IoT/分布式系统
5. v1.3 实现计划有明确的分阶段里程碑

---

## 6. 其他功能增强

以下功能与三大主线并行，穿插在各 Wave 中完成：

### 6.1 Hub 资源目录聚合

**目标：** Hub 角色 peer 维护全局 `resource → available peers` 映射

**实现：**
- Hub 订阅 `dd/{tenant}/peer/resource/report`（已有）
- 新增 `internal/service/resource_catalog.go`：
  - `ResourceCatalog` — `map[resource][]peerID` 倒排索引
  - 随 resource report / heartbeat / unregister 事件更新
- HTTP API 新增：`GET /dd/api/v1/resources/{resource}/providers` — 直接查询 Hub 本地 catalog
- 非 Hub peer 通过 MQ query 查询：发布到 `dd/{tenant}/peer/query`，Hub 回复

### 6.2 幂等性去重

**目标：** sync 请求使用 `IdempotencyKey` 实现去重

**实现：**
- `DdDataService` 新增 `processedIdempotencyKeys map[string]*model.DdMessage`（LRU，max 10000）
- `SendSync` 开始时检查 key 是否已存在，若存在且未超时则直接返回缓存响应
- 响应完成后写入缓存，TTL = 5 * timeout
- 定期清理过期条目

### 6.3 传输状态追踪

**目标：** 每次 sync/async transfer 可查询状态

**实现：**
- 新增 `internal/model/dd_transfer_status.go`：TransferStatus（accepted/routed/delivered/responded/timeout/failed）
- `DdDataService` 新增 `transferStatuses map[requestID]TransferStatus`
- HTTP API 新增：`GET /dd/api/v1/transfer/{requestId}`

---

## 7. v1.2 不做（Out of Scope）

1. Stream data plane 完整实现（仍只保留接口占位）
2. etcd/consul RegistryStore 实际接入
3. 多节点集群部署
4. 完整的 RBAC/多租户权限模型
5. MemMQ 接入
6. 自建 DLQ / 重试调度器
7. gRPC adapter（后续版本考虑）
8. Payload 压缩/加密（后续版本考虑）

---

## 8. 里程碑总览

| Wave | 内容 | 预计周期 | 关键依赖 |
|---|---|---|---|
| **Wave 1** | 配置增强 + HTTP Bridge | 第 1-2 周 | 现有 config 模块 |
| **Wave 2** | CoAP Bridge + MQTT Bridge | 第 3-4 周 | Wave 1 的 Bridge 接口 |
| **Wave 3** | 分布式鉴权设计文档 | 第 5-6 周 | Wave 1/2 的经验反馈 |

### Wave 间依赖

```
Wave 1 (Config + HTTP Bridge)
  ├── Bridge 接口定义 → Wave 2 复用
  └── 配置增强 → Wave 2 的 bridge 配置
          ↓
Wave 2 (CoAP + MQTT Bridge)
  └── 多协议经验 → Wave 3 的 security trade-offs
          ↓
Wave 3 (Distributed Auth Design)
  └── 产出 design doc → v1.3 实现
```

---

## 9. 配置文件目标形态（v1.2 完成时）

```yaml
# config.yaml
server:
  listen_addr: ":8080"

peer:
  id: "hub-01"
  name: "DD Hub 01"
  role: "hub"
  key: ""
  secret: ""

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

bridges:
  http:
    enabled: true
    target_url: "http://localhost:9001"
  coap:
    enabled: false
    target_url: "coap://localhost:5683"
  mqtt_mappings:
    - source: "dd/tenant-a/event/#"
      target: "dd/tenant-b/event/#"

acl:
  rules:
    - peer_id: "*"
      action: "pub"
      pattern: "dd/default/transfer/#"
      allow: true

logging:
  level: "info"
  format: "json"
```

对应的 JSON 格式：

```json
{
  "server": {"listen_addr": ":8080"},
  "peer": {"id": "hub-01", "name": "DD Hub 01", "role": "hub"},
  "mq": {"provider": "mqtt", "mqtt": {"broker_url": "tcp://localhost:1883"}},
  "tenant": "default",
  "bridges": {
    "http": {"enabled": true, "target_url": "http://localhost:9001"},
    "coap": {"enabled": false}
  }
}
```

---

## 10. 下一步启动开发任务 Prompt

将下面 Prompt 直接发给编码代理启动 v1.2 Wave 1 开发：

```text
你现在是 dd-core 的实现代理。请严格按照 docs/plan-v.1.2.md 执行 v1.2 Wave 1 开发
（配置增强 + HTTP 协议桥接），并在每一步给出变更文件与验证结果。

目标：
1) CLI args 增强：--role/--peer-id/--peer-name/--tenant/--listen-addr/
   --mqtt-broker/--log-level/--log-format/--config-json
2) 配置支持 JSON 格式（按扩展名自动识别 .yaml/.json）
3) 优先级：CLI flags > ENV > config file > defaults
4) ProtocolBridge 接口定义（internal/adapter/bridge.go）
5) HTTP Bridge 实现（internal/adapter/http_bridge.go）
6) 重构集成测试使用 HTTPBridge 替换临时桥接代码
7) cmd/main.go 启动 HTTPBridge + 解析 CLI args

硬性约束：
- 不破坏现有测试（go test ./... 通过）
- go build ./cmd/... 通过
- 配置文件格式兼容现有 config.yaml（新增字段有默认值）
- Bridge 接口设计保持 Protocol 无关性（为 Wave 2 的 CoAP/MQTT Bridge 预留）

建议落地步骤：
A. 扩展 internal/config：JSON parser + CLIConfig + LoadPath + MergeJSON
B. 新增 internal/config/cli.go：flag 定义与解析
C. 修改 cmd/main.go：使用 CLI args
D. 新增 internal/adapter/bridge.go + http_bridge.go
E. 修改集成测试使用 HTTPBridge
F. 运行全部测试验证 + go build

输出要求：
1) 列出修改/新增文件
2) 列出运行的测试命令与结果
3) 如果存在待决策项，最后单独列出
```
