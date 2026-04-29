# DD Core v1.0 开发计划

## 1. 版本目标

v1.0 目标是交付一个可运行的、基于 MQTT 的 DD Core 最小可用版本，覆盖：
1. DdPeer 点对点 sync/async 资源请求响应；
2. 统一 `DdMessage` envelope 与 header；
3. MQTT 通用调用接口抽象与接入；
4. 内存服务发现（`DdPeer` 注册/心跳/查询）；
5. sync request/reply（`correlation + timeout`）；
6. peer register/heartbeat/query 全部基于 MQ；
7. topic ACL；
8. 压测基线与故障注入测试。

---

## 2. v1.0 范围（In Scope）

### 2.1 调用模式
- `sync`：request topic + response topic + correlation 匹配 + timeout。
- `async`：event publish/subscribe（幂等由业务侧保证）。

### 2.2 消息模型
- 定义统一 `DdMessage`：
  - `mode`：`sync|async`
  - `protocol`：`http|mq|socket|stream`（v1.0 先落地 `http|mq`）
  - `request_id`
  - `correlation_id`
  - `source_peer_id`
  - `target_peer_id`
  - `resource`
  - `timeout_ms`
  - `headers`
  - `payload`（二进制/JSON）

### 2.3 MQ 接入
- 统一 MQ 接口定义（provider 抽象），v1.0 仅实现 MQTT provider。
- topic 规范采用 MQTT URL 风格：
  - `dd/{tenant}/transfer/{resource}/request`
  - `dd/{tenant}/transfer/{resource}/response`
  - `dd/{tenant}/event/{resource}/publish`
  - `dd/{tenant}/peer/register`
  - `dd/{tenant}/peer/heartbeat`
  - `dd/{tenant}/peer/query`
  - `dd/{tenant}/peer/resource/report`

### 2.4 服务发现
- 内存注册表：`peer_id -> peer meta/resources/status/last_heartbeat_at`。
- 数据来源统一来自 MQ 的 register/heartbeat/report 消息。
- query 可通过 API 返回资源提供者列表。

### 2.5 安全与治理
- topic ACL：按 `client_id|peer_id + topic pattern + action(pub/sub)` 判定。
- deny 优先于 allow。

### 2.6 测试
- 基线压测（吞吐/延迟/超时率）。
- 故障注入（MQ 短暂中断、peer 心跳中断、消息延迟/丢失）。

---

## 3. v1.0 不做（Out of Scope）

1. 持久化存储与恢复机制（交由 MQ）。
2. 自建消息重试调度器/DLQ 系统（交由 MQ）。
3. 自建路由器（router）替代 MQ 分发。
4. stream data plane 完整实现（v1.0 仅保留接口占位，不作为验收主线）。

---

## 4. 里程碑与交付物

## M1：消息模型与 MQTT 接口骨架（第 1 周）
交付物：
1. `DdMessage` 结构与序列化规范文档；
2. `MQClient` 通用接口（publish/subscribe/request/reply）；
3. MQTT provider 基础实现；
4. topic 命名常量与配置。

验收：
- 可通过 MQTT 发布与订阅 `DdMessage`。

## M2：sync/async 调用链路（第 2 周）
交付物：
1. sync request/reply（correlation + timeout）；
2. async publish/subscribe；
3. 协议封装（先 HTTP->HTTP 和 MQ->MQ）；
4. 调用状态与错误码。

验收：
- 同租户下两节点可完成点对点 sync/async。

## M3：Peer 注册与服务发现（第 3 周）
交付物：
1. 基于 MQ 的 peer register/heartbeat/query；
2. 内存服务发现表与资源上报；
3. hub 角色资源目录聚合。

验收：
- peer 下线（心跳超时）后 provider 查询结果可剔除该实例。

## M4：ACL + 测试基线（第 4 周）
交付物：
1. topic ACL（pub/sub）；
2. 压测脚本与指标报告；
3. 故障注入测试脚本与恢复行为报告。

验收：
- ACL 拒绝生效；
- 在故障注入场景下系统行为符合预期（超时、降级、恢复）。

---

## 5. 任务拆解（按模块）

### 5.1 `internal/model`
1. 新增 `DdMessage`、`DdHeader`、`DdTransferMode`。
2. 扩展 `DdPeerInfo`（状态、心跳、资源摘要）。

### 5.2 `internal/service`
1. `DdDataService`：统一入口（sync/async/request matching）。
2. `PeerRegistryService`：注册、心跳、查询、过期清理。
3. `AclService`：topic ACL 校验。

### 5.3 `internal/mq`（建议新增）
1. 定义 `MQClient` 接口。
2. 实现 `mqttClient`。
3. 提供 request/reply helper（基于 correlation_id）。

### 5.4 `api`（建议新增）
1. peer/query API。
2. transfer/sync 与 transfer/async API。
3. ACL 管理 API（最小集）。

### 5.5 `tests`
1. 单元测试：message 编解码、timeout、ACL 规则。
2. 集成测试：两 peer + 一个 hub + 一个 MQTT broker。
3. 压测与故障注入测试：脚本化执行。

---

## 6. 统一验收标准（Definition of Done）

1. sync 请求可在超时窗口内完成 request/reply 匹配；
2. async 请求可通过 MQTT 消费组正常分发；
3. `DdMessage` 契约字段固定并通过契约测试；
4. peer register/heartbeat/query 全部通过 MQ 事件驱动；
5. topic ACL 可阻断未授权 pub/sub；
6. 压测报告和故障注入报告可复现且入库到 `docs/`。

---

## 7. 风险与对策

1. **请求超时与重复响应并存**  
   对策：以 `request_id` 去重，超时后响应按迟到处理记录日志。
2. **ACL 规则复杂度上升**  
   对策：v1.0 仅支持 pattern + action 的最小模型。
3. **服务发现状态漂移**  
   对策：心跳 TTL 与过期扫描统一参数化，可观测化。
4. **MQ 环境差异**  
   对策：用 `MQClient` 接口隔离 provider，仅 v1.0 固化 MQTT。

---

## 8. 下一步启动开发任务 Prompt

将下面 Prompt 直接发给编码代理启动开发：

```text
你现在是 dd-core 的实现代理。请严格按照 docs/plan-v.1.0.md 执行 v1.0 第一阶段开发（M1 + M2 的可编译最小闭环），并在每一步给出变更文件与验证结果。

目标：
1) 实现 sync/async 的 DdPeer 点对点请求响应（先 HTTP->HTTP 与 MQ->MQ）。
2) 定义统一 DdMessage envelope/header（sync|async）。
3) 定义 MQClient 通用接口，并实现 MQTT provider。
4) 实现 sync request/reply（correlation + timeout）。

硬性约束：
- topic 使用 MQTT URL 风格：dd/{tenant}/...
- 不实现持久化、重试调度器、DLQ 引擎（这些依赖 MQ）。
- 不引入 router 角色。
- 代码需可编译，并补最小单测。

建议落地步骤：
A. 新增/完善 model：DdMessage、header、mode 枚举。
B. 新增 internal/mq：MQClient 接口 + mqttClient 实现（publish/subscribe/request/reply helper）。
C. 扩展 internal/service/dd_data_service.go：SendSync/SendAsync 基本流程。
D. 增加 timeout 和 correlation 匹配逻辑。
E. 增加基础测试：message 契约测试、sync timeout 测试、async publish/subscribe 测试。

输出要求：
1) 列出修改文件；
2) 列出运行的测试命令与结果；
3) 如果存在待决策项，最后单独列出。
```
