# DD Core 技术方案设计 v1.1（MQ 原生分发版）

## 1. 文档目的

本文档是 `docs/prd-v1.1.md` 的技术实现方案，重点聚焦：
- 基于 MQ 发布订阅的协议分发机制如何落地；
- sync / async / stream 三种调用在 MQ 上的实现方式；
- 服务发现在该架构中的边界与实现（不引入自建 router）；
- DD Core 与 MQ 各自职责划分。

> 关键原则：DD Core 不重复实现 MQ 已有能力。  
> 包括投递保障、重试、DLQ、消费组、分区并行等，优先复用 MQ 原生机制。

## 2. 设计边界（先明确谁负责什么）

### 2.1 DD Core 负责
1. 各类协议封装与转发（HTTP/RPC/Socket/CoAP/RTSP 等）。
2. 同步、异步、串流三类调用实现（基于 MQ 的调用契约落地）。
3. 各类请求的 timeout 管理（sync/async/stream 控制面超时语义）。
4. `DdPeer` 注册与服务发现（实例、资源、状态管理）。
5. 基于 MQ 的资源请求与响应匹配（`request_id/correlation_id/reply_to`）。
6. 统一 envelope 定义与 profile（`full|minimal|zero`）。
7. topic 规范、消息头规范、trace 规范。
8. 控制面 API（注册、查询、观测）。

### 2.2 MQ 负责
1. 发布订阅分发。
2. 消费组与并行消费。
3. 重试策略、死信队列（若 MQ 支持）。
4. 持久化/确认语义（由 MQ 能力决定）。
5. 分区与吞吐扩展。
6. 并发处理与背压机制（由 MQ 侧提供）。

### 2.3 DD 责任边界（最终确认）

1. **各类协议封装**：DD 负责协议接入、封包、解包与协议间转发，不负责 MQ 内部投递机制。
2. **同步/异步/串流实现**：DD 负责定义并实现三类调用流程与消息契约，底层分发复用 MQ。
3. **各类请求 timeout**：DD 负责请求级超时语义与返回码，MQ 负责消息堆积与投递时序。
4. **DdPeer 注册与服务发现**：DD 负责实例注册、心跳、可用性状态与资源查询。
5. **基于 MQ 的资源请求与响应匹配**：DD 负责 `request_id/correlation_id/reply_to` 的匹配规则与处理流程。

## 3. 总体架构（去 Router 角色）

```text
+------------------+       +-----------------------+       +------------------+
| Ingress Adapter  | ----> | MQ Topics (Pub/Sub)   | ----> | Egress Adapter   |
| HTTP/RPC/RTSP... |       | request/event/stream  |       | protocol rebuild |
+---------+--------+       +-----------+-----------+       +---------+--------+
          |                            |                             |
          v                            v                             v
   +--------------+           +----------------+              +-------------+
   | Control API  | <-------> | Peer Registry  |              | Observability|
   +--------------+           +----------------+              +-------------+
```

说明：
- 数据分发路径只有 **adapter -> MQ -> adapter**。
- DD Core 不在中间再做一层“路由转发进程”。
- 服务发现用于“控制面选型与可观测”，不是替代 MQ 的 topic 分发。

## 4. 分发机制实现

## 4.1 Topic 设计

建议约定：
```text
dd/{tenant}/{domain}/{resource}/{action}
```

典型 topic：
- `dd/default/transfer/order.create/request`
- `dd/default/transfer/order.create/response`
- `dd/default/event/sensor.temp/publish`
- `dd/default/stream/video.rtsp/open`
- `dd/default/stream/video.rtsp/data`
- `dd/default/stream/video.rtsp/close`

设计要点：
1. `resource` 决定业务语义；
2. `action` 决定调用类型（`request/response/event/open/data/close`）；
3. 同步和异步 topic 分离，便于权限和 QoS 管控；
4. stream 的 `data` topic 与 `open/close` 控制 topic 分离。

## 4.2 Sync 调用实现（基于 request/reply）

实现模式：**Request Topic + Reply Topic + Correlation ID**

流程：
1. 调用方发布请求到 `...request` topic；
2. 消费方处理后发布到 `...response` topic；
3. 调用方依据 `correlation_id=request_id` 匹配响应；
4. 超时由调用方或接入层控制，不由 DD Core 自建 waiter 组件抽象。

必需消息头：
- `request_id`
- `correlation_id`（响应中回填请求 ID）
- `reply_to`（可选，指定响应 topic）
- `timeout_ms`

设计约束：
1. sync 语义是“在超时窗口内等待响应”，不是 MQ 原子 RPC；
2. 幂等由业务字段 `idempotency_key` 保证；
3. 若 MQ 不保证顺序，调用方不得依赖多请求严格顺序回包。

## 4.3 Async 调用实现（完全复用 MQ 保障）

实现模式：**Event Topic + Consumer Group**

流程：
1. 发布方写入 event topic；
2. 订阅方以消费组方式消费；
3. 重试、DLQ、ack 策略使用 MQ 原生配置；
4. DD Core 仅记录投递观察指标，不重复实现重试调度器。

建议：
1. 失败处理优先配置在 MQ（重试次数、回退、死信 topic）；
2. DD Core 的控制面只读取并展示这些策略，不重写策略引擎；
3. 业务去重在消费端完成（幂等键）。

## 4.4 Stream 调用实现（minimal/zero）

目标：协议流转发（如 RTSP in -> RTSP out）尽量不做消息切片转换。

### A. 控制面（open/close）
- `stream.open`：携带 `stream_id/resource/profile/subproto`
- `stream.close`：携带 `stream_id/reason`

### B. 数据面（data）
- `profile=minimal`：首包或关键帧保留最小元数据；
- `profile=zero`：后续数据包仅带 `stream_id + payload` 必要字段；
- 不引入额外业务字段映射，不做二次封装协议转换。
- stream 场景默认允许数据丢失（丢帧/丢片可接受），DD Core 不提供补偿重传。

实现建议：
1. `open` 成功后写入本地 `stream_session` 映射；
2. `data` 包根据 `stream_id` 转发到目标协议连接；
3. DD Core 仅做连接生命周期管理与指标暴露。

## 4.5 跨系统“同协议”请求接口机制（关键）

原则：**请求进来是什么协议，跨系统出去仍然是什么协议**。  
DD 不把调用语义降级成“统一 HTTP”，而是保留协议形态，只借助 MQ 做跨系统传递与匹配。

1. HTTP -> HTTP  
   - 客户端以 HTTP 调用本地 DD；
   - DD 将 HTTP 请求封装为 MQ request 消息；
   - 目标网络 DdPeer 解包后仍发起 HTTP 请求到目标服务；
   - 响应再按 MQ response 回传，入口 DD 还原 HTTP 响应。
2. MQ -> MQ  
   - 客户端本身就是 topic 发布/订阅；
   - DD 仅做 topic 映射与跨网络桥接，不改变 MQ 语义。
3. Socket -> Socket  
   - 请求与响应都按 socket 连接语义处理；
   - DD 只负责封装连接标识、会话标识与 payload。
4. Stream(如 RTSP) -> Stream(RTSP)  
   - 控制消息经 MQ（open/close）；
   - 数据流按 stream profile 转发，不转成业务消息切片。

跨系统请求统一消息头（最小集）：
- `protocol`：`http|mq|socket|stream`
- `request_id`
- `correlation_id`
- `source_peer_id`
- `target_peer_id`
- `resource`
- `timeout_ms`

## 4.6 DdPeer 资源上报到 Hub（通过 MQ）

约束：每个 DdPeer 的资源清单（API、topics、stream endpoints 等）必须上报给 `role=hub` 的 DdPeer，且上报通道统一走 MQ。

### A. 上报 topic
```text
dd/{tenant}/peer/resource/report
dd/{tenant}/peer/resource/heartbeat
dd/{tenant}/peer/resource/offline
```

### B. 上报消息结构（示例）
```json
{
  "peer_id": "edge-01",
  "role": "edge",
  "timestamp": "2026-04-26T22:00:00Z",
  "resources": {
    "apis": [{"name":"order.query","protocol":"http","path":"/order/{id}","method":"GET"}],
    "topics": [{"name":"iot.temp","mode":"pub"}],
    "streams": [{"name":"camera.main","protocol":"rtsp","path":"rtsp://..."}]
  }
}
```

### C. Hub 处理逻辑
1. Hub 订阅 `dd/+/peer/resource/#` topic；
2. 以 `peer_id` 为键维护资源目录（内存 + 可选外存）；
3. 通过 heartbeat 更新资源有效期；
4. peer 下线或超时时将其资源标记不可用；
5. 对外提供 `resource -> available peers` 查询。

### D. 失败与重传
- 上报重试、死信、堆积处理完全依赖 MQ 策略；
- DD 只定义上报消息格式和 hub 消费逻辑。

## 5. 服务发现实现（与 MQ 分工明确）

你指出的点是正确的：发布订阅分发不需要 router。  
服务发现的职责应是“**谁提供哪个资源 + 当前是否可用**”，不是“代替 MQ 分发消息”。

## 5.1 服务发现数据模型

`PeerInstance`：
- `peer_id`
- `name`
- `role`
- `status` (`active|stale|offline`)
- `resources[]`
- `capabilities[]`
- `last_heartbeat_at`

## 5.2 注册与续约

1. `POST /peers/register`：写入实例和资源声明；
2. `POST /peers/{id}/heartbeat`：续约；
3. 定时剔除超时实例并标记 `stale/offline`；
4. 查询接口返回可用实例列表与元数据。

## 5.3 服务发现在数据路径中的用法

1. **不做消息级路由**：消息仍由 MQ topic + 订阅关系分发。
2. **做治理增强**：
   - API 层校验目标 resource 是否存在可用 provider；
   - 控制面展示 resource -> peer 映射；
   - 发生故障时用于排障与流量切换决策。

## 5.4 实现方案选择

v1.1 推荐：
- 内置内存注册表（简单、低延迟、单节点优先）。

v1.2+ 演进：
- 抽象 `RegistryStore` 后切换 etcd/consul，支持多节点共享视图。

## 6. 可靠性与性能职责矩阵

| 能力 | 归属 |
|---|---|
| 发布订阅分发 | MQ |
| 并发处理能力 | MQ |
| 重试/DLQ | MQ（优先） |
| 消费组并行 | MQ |
| 持久化与可恢复性 | MQ |
| 协议编解码 | DD Core Adapter |
| sync request/reply 协议约定 | DD Core |
| stream profile 优化 | DD Core |
| 服务实例注册与查询 | DD Core（v1.1） |

## 7. 控制面 API（落地视角）

基路径：`/dd/api/v1`

1. `POST /peers/register`
2. `POST /peers/{id}/heartbeat`
3. `GET /peers/{id}`
4. `GET /peers?resource=&status=&capability=`
5. `POST /resources/register`
6. `GET /resources/{resource}/providers`
7. `POST /hub/resources/report`（可选：本地 API 触发一次资源全量上报到 MQ）
8. `GET /hub/resources/catalog`（仅 hub 提供：查看资源目录）
9. `POST /transfer/sync`
10. `POST /transfer/async`
11. `POST /transfer/stream/open`
12. `POST /transfer/stream/close`
13. `GET /transfer/{requestId}`

说明：
- `/transfer/async` 只做发布与追踪，不承诺自建重试；
- `/transfer/sync` 负责 request/reply 契约，不引入独立 waiter 角色定义。

## 8. 配置建议

```yaml
mq:
  provider: "kafka|nats|pulsar|rabbitmq"
  request_topic_prefix: "dd/default/transfer"
  event_topic_prefix: "dd/default/event"
  stream_topic_prefix: "dd/default/stream"
  resource_report_topic: "dd/default/peer/resource/report"
  resource_heartbeat_topic: "dd/default/peer/resource/heartbeat"

sync:
  default_timeout_ms: 3000
  reply_topic_mode: "fixed|per-client"

stream:
  control_profile: "minimal"
  data_profile: "zero"
  idle_timeout_ms: 15000

discovery:
  heartbeat_interval_sec: 10
  lease_ttl_sec: 30
  stale_after_miss: 3
```

## 9. 工程实现清单（按优先级）

### P0
1. 定义统一 envelope 与 header 规范（含 sync/async/stream）。
2. 打通 adapter -> MQ -> adapter 基础链路。
3. 实现 sync request/reply（correlation + timeout）。
4. 实现 peer register/heartbeat/query。

### P1
1. stream open/data/close 与 `minimal/zero` profile。
2. 观测指标：topic 延迟、响应超时率、stream 活跃会话、stream 丢包计数。
3. 资源查询接口与 provider 可视化。

### P2
1. 抽象 `RegistryStore`（为 etcd/consul 演进做准备）。
2. 完善权限与 topic ACL。
3. 统一压测基线与故障注入测试。

## 10. 待你拍板的技术决策

1. **MQ 类型**：Kafka / NATS / Pulsar / RabbitMQ（会影响 reply 模式和 stream 实现细节）。
2. **sync reply topic 策略**：固定 response topic 还是按 client 独立 topic。
3. **stream data 负载形态**：是否允许完全裸二进制 payload（推荐允许）。
4. **服务发现存储**：v1.1 是否仅内存，还是立即引入 etcd。
5. **异步失败策略**：建议固定为“完全依赖 MQ 策略”，DD Core 不再提供策略层配置。

---

如果你同意，我下一步可以直接补一版 `docs/design-sequence-v1.1.md`，只放三张时序：
1) sync request/reply，2) async pub/sub + MQ retry，3) register/heartbeat/service discovery。
