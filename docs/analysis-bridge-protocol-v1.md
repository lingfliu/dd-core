# dd-core 数据桥接协议缺陷与性能瓶颈分析报告

> 分析日期：2026-04-29
> 分析范围：`internal/adapter/`, `internal/model/`, `internal/service/`, `internal/mq/`, `internal/config/`, `api/`, `internal/observability/`

---

## 目录

- [一、架构层面缺陷](#一架构层面缺陷)
- [二、协议设计缺陷](#二协议设计缺陷)
- [三、实现层面缺陷](#三实现层面缺陷)
- [四、性能瓶颈](#四性能瓶颈)
- [五、MQ 消息分发机制下的带宽影响分析](#五mq-消息分发机制下的带宽影响分析)
- [六、可靠性与可观测性缺陷](#六可靠性与可观测性缺陷)
- [七、改进建议摘要](#七改进建议摘要)

---

## 一、架构层面缺陷

### 1.1 MQTT Broker 单点故障

所有数据传输、桥接、对等体发现均依赖单一 MQTT Broker。Broker 宕机后整个系统瘫痪，没有集群或故障转移机制。

**相关代码：** `cmd/main.go` 中 MQTT 客户端构建，仅连接单个 Broker URL。

### 1.2 Hub-and-Spoke 瓶颈

所有消息都必须经过中心 Hub（MQTT Broker），缺乏 peer-to-peer 直连通道。对于大数据量场景（如视频流、批量传感器数据），中心 Broker 带宽成为瓶颈。

### 1.3 Bridge 缺乏生命周期管理

`ProtocolBridge` 接口只有 `Start()` 方法，没有 `Stop()` / `Shutdown()`。主进程退出时 Bridge 无法优雅关闭，可能丢失正在处理的消息。

**相关代码：** `internal/adapter/bridge.go` — 接口定义仅包含 `Start(ctx)` 和 `Protocol()`。

### 1.4 Stream 协议与 Bridge 完全割裂

`DdProtocolStream` 作为协议常量已定义，但：
- 没有任何 `StreamBridge` 实现
- `StreamService` 完全独立于 Bridge 体系运行
- Stream 与 Transfer 使用不同的 Topic、不同的数据模型，之间无法互通

**相关代码：** `internal/model/dd_message.go` 中 `DdProtocolStream` 常量；`internal/service/stream_service.go` 完全独立实现。

---

## 二、协议设计缺陷

### 2.1 🔴 严重：Resource 与 PeerId 语义混淆导致 Topic 路由不匹配

这是最严重的设计缺陷。

- **API Handler** 发布消息到 `dd/{tenant}/transfer/{resource}/request`
- **Bridge** 订阅 `dd/{tenant}/transfer/{peerId}/request`

两者都使用了 `TransferRequest()` 方法但传入不同语义的参数：Handler 传 Resource 名，Bridge 传 PeerId。这意味着只有当 `resource == peerId` 时消息才能被路由到正确的 Bridge。Resource（逻辑资源标识）和 PeerId（物理节点标识）被强行等同，破坏了服务发现与寻址的分离。

**相关代码：**
- `api/handler.go` — `handleTransferSync` 使用 `s.topics.TransferRequest(req.Resource)`
- `internal/adapter/http_bridge.go` — `Start()` 使用 `b.topics.TransferRequest(b.peerId)`
- `internal/adapter/coap_bridge.go` — `Start()` 使用 `b.topics.TransferRequest(b.peerId)`

### 2.2 无协议版本号

`DdMessage` 结构体没有 `version` 字段，无法在演进协议时保持向后兼容。

**相关代码：** `internal/model/dd_message.go` — `DdMessage` 结构体定义。

### 2.3 Payload 完全不透明

`Payload` 是 `[]byte`，缺乏 Content-Type 协商、Schema 校验、编码标识。所有消费者需要带外约定 Payload 格式，不具备自描述能力。

### 2.4 双轨 Header 设计混乱

`DdMessage` 同时包含：
- `Header DdHeader` — 结构化元数据（RequestId, CorrelationId 等）
- `Headers map[string]string` — 自由格式的协议相关头部（method, path, content-type）

两个 Header 层级界定不清。例如 HTTP method/path 被放入 `Headers` 而非协议无关的标准字段，而 `Header.TimeoutMs` 却被 CoAP Bridge 硬编码的 5 秒覆盖忽略。

### 2.5 缺乏 QoS / 优先级机制

协议层面没有 QoS 等级、消息优先级、交付保证等概念，完全依赖底层 MQTT 的 QoS。

### 2.6 Reply Topic 硬编码默认租户

CoAP Bridge 和 HTTP Bridge 中的 fallback reply topic 硬编码为 `"dd/default/transfer/%s/response"`，无视实际配置的 tenant，多租户场景下会路由到错误的 Topic。

**相关代码：**
- `internal/adapter/coap_bridge.go` — `handleSyncRequest` 中 reply topic 默认值
- `internal/adapter/http_bridge.go` — `handleSyncRequest` 中 reply topic 默认值

---

## 三、实现层面缺陷

### 3.1 CoAP Bridge 硬编码超时

CoAP Bridge 的 UDP 读写固定使用 5 秒超时，完全忽略 `msg.Header.TimeoutMs`。

**相关代码：** `internal/adapter/coap_bridge.go` — `doCoap()` 方法中 `conn.SetDeadline(time.Now().Add(5 * time.Second))`。

### 3.2 CoAP Bridge 每次请求新建 UDP 连接

每次 `doCoap` 调用都执行 `net.DialUDP` + `conn.Close()`，包括 DNS 解析开销。高并发下性能极差。

**相关代码：** `internal/adapter/coap_bridge.go` — `doCoap()` 方法中每请求建连。

### 3.3 CoAP 消息 ID 碰撞风险

使用 `time.Now().UnixNano() % 65536` 生成消息 ID，在纳秒级并发下碰撞概率显著。

**相关代码：** `internal/adapter/coap_bridge.go` — `buildCoapRequest()` 函数。

### 3.4 CoAP Option 编码 UTF-8 Bug

`len(val)-1` 使用字节长度而非字符长度，含多字节 UTF-8 字符的路径会生成错误的 CoAP Option Length。

**相关代码：** `internal/adapter/coap_bridge.go` — `encodeCoapOption()` 函数。

### 3.5 CoAP Option Length 解析错误

`int(data[pos]&0x0f) + 1` 总是对 length +1，但 CoAP 规范中 length=0 就表示长度为 0（用于 option delta 编码），不应 +1。

**相关代码：** `internal/adapter/coap_bridge.go` — `parseCoapPayload()` 函数。

### 3.6 HTTP Bridge 丢失 HTTP 状态码

只返回响应 Body，状态码被丢弃。调用方无法区分 200 OK 和 500 错误。

**相关代码：** `internal/adapter/http_bridge.go` — `doHttp()` 方法返回值仅为 body bytes。

### 3.7 无重试 / 退避机制

HTTP Bridge 和 CoAP Bridge 均无重试逻辑。网络抖动导致的瞬时故障直接返回错误。

### 3.8 MQTT Bridge 忽略 Context

转发时使用 `context.Background()` 而非从 handler 传入的 context，导致追踪链路断裂、取消信号无法传播。

**相关代码：** `internal/adapter/mqtt_bridge.go` — handler 闭包内 `b.mqClient.Publish(context.Background(), ...)`。

### 3.9 幂等性实现粗糙

- 最大容量 10000，超限后随机删除，无 LRU/LFU 淘汰策略
- TTL 为 `timeout * 5`，缺乏合理语义

**相关代码：** `internal/service/dd_data_service.go` — `sweepIdempotent()` 方法。

### 3.10 Transfer Status 内存泄漏

`transferStatuses` map 从未清理，每次传输都追加一条记录，长时间运行必然 OOM。

**相关代码：** `internal/service/dd_data_service.go` — `recordStatus()` 方法只写不删。

### 3.11 Bridge Handler 不做消息校验

HTTP/CoAP Bridge 的 handler 只检查 Protocol 字段，不调用 `msg.Validate()`，非法消息不会在入口被拦截。

**相关代码：**
- `internal/adapter/http_bridge.go` — `handleSyncRequest()` 和 `handleAsyncEvent()`
- `internal/adapter/coap_bridge.go` — `handleSyncRequest()` 和 `handleAsyncEvent()`

### 3.12 Stream Session 无人清理

`StreamService.SweepIdle()` 定义了闲置超时清理逻辑，但没有任何地方调用它。Stream Session 会无限累积。

**相关代码：** `internal/service/stream_service.go` — `SweepIdle()` 方法；`cmd/main.go` 中缺失调用。

### 3.13 Sync Response Channel 竞态条件

当 `waitCh` 已满时使用 `default:` 丢弃响应。如果响应恰好在超时之后、清理之前到达，会被静默丢弃且无法恢复。

**相关代码：** `internal/service/dd_data_service.go` — `SubscribeSyncResponses()` 中的 select 语句。

---

## 四、性能瓶颈

### 4.1 JSON 序列化成为 CPU 热点

所有消息经过 MQTT 时都要做 `json.Marshal` / `json.Unmarshal`。对于高频小消息场景，JSON 的序列化开销远高于 Protobuf / MessagePack 等二进制协议。

### 4.2 同步请求 4 跳 MQTT 延迟

一次 Sync 请求的路径：

```
API Handler → Publish → Broker → Subscribe → Bridge → 外部调用
→ 响应构造 → Publish → Broker → Subscribe → Response Channel
```

至少 4 次 MQTT 中转，每跳增加数毫秒延迟。

### 4.3 CoAP Bridge 串行化处理

`doCoap` 是阻塞 I/O（UDP write + read）。如果 MQTT client 的消息回调是串行分发的，那么一个 CoAP 请求的 RTT 会阻塞所有后续请求。

### 4.4 全局锁竞争

`DdDataService` 中 `pendingMu`、`statusMu`、`idempotentMu` 在高并发下是热点锁，尤其是 `pendingMu` 在 sync 请求的整个生命周期中多次加锁。

**相关代码：** `internal/service/dd_data_service.go` — `SendSync()` 方法中对 `pendingMu` 的多次操作。

### 4.5 HTTP Bridge 连接池未优化

使用默认 `http.Client` 和默认 Transport，未调整 `MaxIdleConns`、`MaxIdleConnsPerHost`、`IdleConnTimeout` 等关键连接池参数。

**相关代码：** `internal/adapter/http_bridge.go` — `NewHttpBridge()` 中 `http.Client` 构造。

### 4.6 无消息批处理

不支持批量发送，每个 `DdMessage` 独立 Publish，无法摊销 MQTT 单次传输的固定开销。

### 4.7 无反压机制

当 Bridge 下游设备处理慢时，MQTT 消息堆积在 Broker 中，没有流量控制信号反向通知生产者降速。

### 4.8 CoAP 响应被 1500 字节 MTU 截断

读取缓冲区固定 1500 字节，不支持 CoAP Block-wise Transfer，大响应被截断。

**相关代码：** `internal/adapter/coap_bridge.go` — `doCoap()` 中 `buf := make([]byte, 1500)`。

---

## 五、MQ 消息分发机制下的带宽影响分析

当前系统完全基于 MQTT Broker 进行消息路由和分发，所有数据（包括控制面信息、同步请求/响应、异步事件、流事件、心跳、对等体发现广播等）都经过中心 Broker 中转。以下从多个维度分析该机制对带宽的实际影响。

### 5.1 数据传输路径与跳数放大

#### 5.1.1 同步请求 (Sync) 的完整网络路径

```
请求方向:
  API Handler ──PUBLISH──▶ MQTT Broker ──PUBLISH──▶ Bridge Handler
                     PUBACK◀──                     ──PUBACK▶

响应方向:
  Bridge Handler ──PUBLISH──▶ MQTT Broker ──PUBLISH──▶ Response Subscriber
                         PUBACK◀──                     ──PUBACK▶
```

一次完整的 Sync 请求在 MQ 层面产生 **4 次 PUBLISH + 4 次 PUBACK = 8 次网络报文传输**。  

**相关代码：**
- `internal/service/dd_data_service.go` — `SendSync()` 发布请求到 `requestTopic`
- `internal/adapter/http_bridge.go` — `handleSyncRequest()` 发布响应到 `replyTopic`
- `internal/mq/mqtt_client.go` — `Publish()` 使用 QoS 1，每次发布需等待 PUBACK

#### 5.1.2 异步事件 (Async) 的网络路径

```
API Handler ──PUBLISH──▶ MQTT Broker ──PUBLISH──▶ Bridge Handler
                   PUBACK◀──                     ──PUBACK▶
```

一次 Async 请求产生 **2 次 PUBLISH + 2 次 PUBACK = 4 次网络报文传输**。

### 5.2 JSON 信封膨胀比分析

`DdMessage` 的 JSON 序列化包含大量结构化元数据字段。以一典型同步请求为例：

```json
{
  "mode": "sync",
  "protocol": "http",
  "resource": "sensor/temperature",
  "header": {
    "request_id": "req-20260102150405.123456789",
    "correlation_id": "",
    "reply_to": "dd/default/transfer/sensor-temp/response",
    "timeout_ms": 3000,
    "source_peer_id": "edge-sensor-01",
    "target_peer_id": "edge-hub-01",
    "tenant": "default",
    "trace_id": "",
    "idempotency_key": ""
  },
  "headers": {
    "method": "GET",
    "path": "/api/v1/sensor/temperature"
  },
  "payload": "eyJ0ZW1wZXJhdHVyZSI6IDIzLjV9",
  "created_at": "2026-01-02T15:04:05.123456789Z"
}
```

**信封体积量化：**

| Payload 原始大小 | JSON 信封大小 | 总传输大小 | 开销比例 |
|-----------------|--------------|-----------|---------|
| 0 字节（空）     | ~450 字节    | ~450 字节 | ∞       |
| 50 字节（传感器读数） | ~450 字节 | ~530 字节 | **10.6x** |
| 200 字节（状态快照） | ~450 字节 | ~680 字节 | **3.4x** |
| 1 KB（配置下发）   | ~450 字节 | ~1.5 KB   | **1.5x** |
| 10 KB（固件块）   | ~450 字节 | ~10.5 KB  | 1.05x   |

对于 IoT 场景中最常见的小消息传输（传感器读数、状态上报、指令下发），信封开销高达 **3x ~ 10x**，是当前系统最主要的带宽浪费来源。

**相关代码：** `internal/model/dd_message.go` — `DdMessage` 结构体，`Header DdHeader` 包含 9 个字段，所有字段通过 `json.Marshal` 序列化后传输。

### 5.3 MQTT 协议层级开销

基于 [mqtt_client.go](file:///Users/lingfengliu/git/dd-core/internal/mq/mqtt_client.go) 的实现，每次 `Publish` 使用 **QoS 1**（至少一次送达）：

| MQTT 报文组成 | 字节数 | 说明 |
|--------------|--------|------|
| 固定头 (Fixed Header) | 2 字节 | 控制报文类型 + 剩余长度 |
| 可变头 - Topic Name | 35~50 字节 | 如 `dd/default/transfer/sensor-temp/request` |
| 可变头 - Packet Identifier | 2 字节 | QoS 1 所需的消息 ID |
| Payload | N 字节 | JSON 序列化后的 DdMessage |
| **PUBACK 响应** | 4 字节 | 每个 QoS 1 发布的确认包 |

以租户 `default`、资源名 `sensor-temp` 为例：
- Topic: `dd/default/transfer/sensor-temp/request` = 40 字节
- 单次 PUBLISH 协议开销: 2 + 40 + 2 = **44 字节**
- PUBACK: **4 字节**
- 单次发布的总传输: 44 + 4 + Payload 大小

### 5.4 端到端带宽消耗计算

以一条 50 字节的传感器温度读数（payload `{"temperature": 23.5}`）为例：

#### Sync 模式端到端带宽

| 阶段 | 方向 | 传输内容 | 字节数 |
|------|------|---------|--------|
| 1 | API → Broker | MQTT PUBLISH (QoS 1) | 44 + 530 = 574 |
| 2 | Broker → API | MQTT PUBACK | 4 |
| 3 | Broker → Bridge | MQTT PUBLISH (QoS 1) | 44 + 530 = 574 |
| 4 | Bridge → Broker | MQTT PUBACK | 4 |
| 5 | Bridge → Broker | MQTT PUBLISH (响应, QoS 1) | 44 + 530 = 574 |
| 6 | Broker → Bridge | MQTT PUBACK | 4 |
| 7 | Broker → Subscriber | MQTT PUBLISH (QoS 1) | 44 + 530 = 574 |
| 8 | Subscriber → Broker | MQTT PUBACK | 4 |
| **合计** | | | **2,312 字节** |

**有效数据占比：50 / 2312 ≈ 2.2%**，即带宽利用率仅约 2%。

#### Async 模式端到端带宽

| 阶段 | 方向 | 传输内容 | 字节数 |
|------|------|---------|--------|
| 1 | API → Broker | MQTT PUBLISH (QoS 1) | 44 + 530 = 574 |
| 2 | Broker → API | MQTT PUBACK | 4 |
| 3 | Broker → Bridge | MQTT PUBLISH (QoS 1) | 44 + 530 = 574 |
| 4 | Bridge → Broker | MQTT PUBACK | 4 |
| **合计** | | | **1,156 字节** |

**有效数据占比：50 / 1156 ≈ 4.3%**。

### 5.5 后台流量对带宽的持续消耗

除数据传输外，以下控制面流量持续消耗带宽：

| 流量类型 | Topic 格式 | 周期 | 估算单次大小 |
|---------|-----------|------|------------|
| 对等体心跳 | `dd/{tenant}/peer/heartbeat` | 每 10s（默认） | ~120 字节 |
| Hub 广播 | `dd/{tenant}/peer/hub/broadcast` | 每 10s（默认） | ~250 字节 |
| Auth 广播 | `dd/{tenant}/peer/auth/broadcast` | 按需 | ~200 字节 |
| 对等体资源上报 | `dd/{tenant}/peer/resource/report` | 按需 | ~500+ 字节 |
| 流事件 | `dd/{tenant}/stream/{res}/open\|close` | 按需 | ~350 字节 |

以 10 个 edge peer + 1 个 hub 的典型部署为例，每 10 秒的心跳和广播流量约为：

```
(10 edge peers × 120 bytes × 2 publish) + (1 hub × 250 bytes × 2 publish) 
= 2400 + 500 = 2900 bytes/10s ≈ 290 bytes/s 持续消耗
```

**相关代码：**
- `internal/service/topics.go` — 所有 Topic 定义
- `internal/config/config.go` — `BroadcastConfig.IntervalSec` 默认为 10 秒
- `internal/service/hub_discovery.go` — Hub 广播服务
- `internal/model/dd_peer_events.go` — 心跳和资源上报事件结构体

### 5.6 桥接层的双编码损耗

CoAP Bridge 在内部路由和外部通信之间经历了一次不必要的编解码转换：

```
DdMessage (JSON) ──MQTT──▶ CoAP Bridge ──JSON.Unmarshal──▶ 
──buildCoapRequest (二进制 CoAP)──▶ UDP ──▶ CoAP 设备
```

设备端实际只需要二进制 CoAP 报文，但系统内部仍以 JSON 形式在 MQTT 中传输。理想路径应为：

```
DdMessage (Binary) ──MQTT──▶ CoAP Bridge ──直接转发或最小转换──▶ CoAP 设备
```

**相关代码：**
- `internal/adapter/coap_bridge.go` — `handleSyncRequest()` 先 JSON 反序列化再构建二进制 CoAP 包
- `internal/adapter/coap_bridge.go` — `buildCoapRequest()` 构建 CoAP 二进制报文

### 5.7 MQTT Client 配置的带宽相关缺陷

[mqtt_client.go](file:///Users/lingfengliu/git/dd-core/internal/mq/mqtt_client.go) 中的 `NewMqttClient` 存在以下配置缺失，直接影响带宽效率：

| 缺失配置项 | 影响 |
|-----------|------|
| 未设置 `SetMaxReconnectInterval` | 断连后默认以 10 分钟为上限重试，期间消息积压 |
| 未设置 `SetWriteTimeout` | 写超时默认 30s，慢速网络下写阻塞 |
| 未启用 MQTT 5.0 特性 | 无法使用 Payload Format Indicator、Content Type、Topic Alias 等带宽优化特性 |
| 未配置 `SetKeepAlive` | 默认 30s 保活，低频网络下可增大以减少 PINGREQ/PINGRESP 开销 |

### 5.8 改进建议

#### 🔴 P0 — 立即修复

| 建议 | 说明 | 预期带宽节省 |
|------|------|------------|
| 启用 Payload 压缩 | 在 `DdDataService` 层对 Payload 进行 gzip/zstd 压缩后再序列化 | 小消息 30-50%，大消息 60-80% |
| 移除 JSON omitempty 空字段的传输 | 当前 `omitempty` 标记已存在，但 MQTT 层不做精简；确认 MQTT QoS 降级为 QoS 0 的场景 | 每消息节省 50-150 字节 |

#### 🟠 P1 — 短期优化

| 建议 | 说明 | 预期带宽节省 |
|------|------|------------|
| 引入二进制序列化协议 | 用 Protobuf/MessagePack 替代 JSON 作为 DdMessage 的传输编码 | 每消息节省 40-60% 信封体积 |
| 消息批处理 | 在 `mq.Client` 接口层增加 `PublishBatch()`，将多个小消息合并为一次 MQTT PUBLISH | 节省 (n-1) × 44 字节 MQTT 报头 |
| MQTT Topic Alias | 升级到 MQTT 5.0，启用 Topic Alias 将长 Topic 映射为 2 字节整数 | 每消息节省 ~38 字节 Topic 传输 |
| 分层 QoS 策略 | Sync 请求使用 QoS 1，Async 事件和 Heartbeat 使用 QoS 0 | Async 每消息省 4 字节 PUBACK |

#### 🟡 P2 — 中期优化

| 建议 | 说明 | 预期效果 |
|------|------|---------|
| 响应直连通道 | 对于 Sync 响应，Bridge 直接通过 HTTP/CoAP 回调而非再次经过 MQTT | Sync 减少 2 次 MQTT 跳转 |
| CoAP Bridge 内部二进制转发 | CoAP Bridge 直接转发二进制 CoAP 报文，不经 JSON 序列化 | 消除 CoAP 路径的双编码损耗 |
| DdMessage 字段精简 | 按传输场景裁剪 DdMessage 字段（Async 不需要 ReplyTo/CorrelationId 等） | 每消息节省 80-150 字节 |
| 动态 Topic 命名 | 缩短 Topic 层级，如 `dd/d/t/{res}/req` 替代 `dd/{tenant}/transfer/{resource}/request` | 每消息节省 15-20 字节 |

#### 🟢 P3 — 长期演进

| 建议 | 说明 |
|------|------|
| MQTT 5.0 全面迁移 | 利用 Payload Format Indicator、Content Type、User Properties、Session Expiry 等特性 |
| Peer-to-Peer 数据面 | 对于大数据量流式传输，协商 WebRTC/QUIC 直连，旁路 Broker |
| 边缘消息聚合 | 在 Edge Peer 侧实现本地消息聚合，定时批量上报 |
| 自适应压缩策略 | 根据 Payload 大小自动选择 noop/gzip/zstd，平衡 CPU 与带宽 |

---

## 六、可靠性与可观测性缺陷

### 6.1 缺乏死信队列

桥接失败的消息只记日志后丢弃，无法事后重放或排查。

### 6.2 缺乏熔断器

下游设备持续故障时 Bridge 仍会尝试每次请求，耗尽系统资源。

### 6.3 Bridge 侧零可观测性

Prometheus 指标只覆盖 `DdDataService` 层。Bridge 层的操作（HTTP 状态码分布、CoAP 延迟、桥接错误率等）完全不可见。

**相关代码：** `internal/observability/metrics.go` — 缺乏 Bridge 层指标。

### 6.4 缺乏 TLS / mTLS

HTTP Bridge 和 CoAP Bridge 都不支持 TLS 加密通信，在跨网络安全场景下存在风险。

### 6.5 TraceId 无实际作用

`DdHeader.TraceId` 字段已定义，但：
- API Handler 构建消息时未设置 TraceId
- Bridge 构建响应时未传播 TraceId
- 没有任何分布式追踪集成（如 OpenTelemetry）

---

## 七、改进建议摘要

| 优先级 | 类别 | 建议 |
|--------|------|------|
| 🔴 P0 | 协议设计 | 统一 Resource/PeerId 语义，分离寻址与服务发现 |
| 🔴 P0 | 内存安全 | 为 transferStatuses 添加 TTL 淘汰机制 |
| 🔴 P0 | 协议设计 | 添加协议版本号字段 |
| 🔴 P0 | 带宽 | 启用 Payload gzip/zstd 压缩（小消息节省 30-50% 带宽） |
| 🟠 P1 | 实现修复 | CoAP 硬编码超时改为读取 msg.Header.TimeoutMs |
| 🟠 P1 | 实现修复 | CoAP UDP 连接池化，避免每请求建连 |
| 🟠 P1 | 实现修复 | HTTP Bridge 保留并传播状态码 |
| 🟠 P1 | 可靠性 | Bridge 接口添加 Stop() 方法实现优雅关闭 |
| 🟠 P1 | 可观测性 | 添加 Bridge 层 Prometheus 指标（延迟/错误率/状态码） |
| 🟠 P1 | 实现修复 | Stream SweepIdle 需要被周期性调用 |
| 🟠 P1 | 带宽 | 引入 Protobuf/MessagePack 替代 JSON（信封体积节省 40-60%） |
| 🟠 P1 | 带宽 | 消息批处理合并小消息为一次 MQTT PUBLISH |
| 🟠 P1 | 带宽 | 分层 QoS：Async/Heartbeat 降为 QoS 0，Sync 保持 QoS 1 |
| 🟡 P2 | 性能 | 支持消息批处理减少 MQTT 报文开销 |
| 🟡 P2 | 可靠性 | 添加重试/退避机制和熔断器 |
| 🟡 P2 | 可靠性 | 添加死信队列 |
| 🟡 P2 | 功能 | 实现 Stream Bridge 或移除 DdProtocolStream |
| 🟡 P2 | 带宽 | Sync 响应通过直连通道回调，减少 2 次 MQTT 跳转 |
| 🟡 P2 | 带宽 | DdMessage 按传输场景精简字段 |
| 🟡 P2 | 带宽 | 升级 MQTT 5.0 并启用 Topic Alias |
| 🟢 P3 | 安全 | 支持 TLS/mTLS |
| 🟢 P3 | 协议设计 | 为 Payload 添加 Content-Type 自描述能力 |
| 🟢 P3 | 可观测性 | 集成分布式追踪（OpenTelemetry） |
| 🟢 P3 | 带宽 | Peer-to-Peer 直连数据面旁路 Broker（大数据量流式传输） |
| 🟢 P3 | 带宽 | 边缘消息聚合 + 自适应压缩策略 |

---

## 附录：核心代码文件索引

| 文件 | 说明 |
|------|------|
| `internal/adapter/bridge.go` | 协议桥接接口定义 |
| `internal/adapter/http_bridge.go` | HTTP 协议桥接实现 |
| `internal/adapter/coap_bridge.go` | CoAP 协议桥接实现 |
| `internal/adapter/mqtt_bridge.go` | MQTT 协议桥接实现 |
| `internal/model/dd_message.go` | 核心协议消息与头部定义 |
| `internal/model/dd_transfer_status.go` | 传输状态模型 |
| `internal/model/dd_stream.go` | 流协议模型 |
| `internal/model/dd_errors.go` | 错误码定义 |
| `internal/service/dd_data_service.go` | 数据传输服务（SendSync/SendAsync） |
| `internal/service/stream_service.go` | 流服务 |
| `internal/service/topics.go` | Topic 路由定义 |
| `internal/config/config.go` | Bridge 配置结构 |
| `internal/mq/client.go` | 消息队列抽象接口 |
| `internal/observability/metrics.go` | Prometheus 指标定义 |
| `api/handler.go` | HTTP API 请求处理器 |
| `api/router.go` | 路由注册 |
| `cmd/main.go` | 应用入口，服务组装 |
