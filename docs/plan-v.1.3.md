# DD Core v1.3 开发计划

## 1. 版本目标

v1.3 的目标是在不破坏现有 Broker 拓扑前提下，完成三类升级：

1. **传输效率升级**：同步链路从阻塞 RPC 演进到异步关联响应，降低跳数与时延；
2. **协议信封升级**：`DdMessage` 从“双 Header 混用”演进到“传输 meta + 协议专属 meta”；
3. **资源治理升级**：`DdPeer` 对资源请求元数据进行声明式注册，并在 API/Bridge 入口做契约校验。

该计划基于 `docs/design-v.1.3.md` 与 `docs/analysis-bridge-protocol-v1.md`，并要求：

- 对两份文档做到全覆盖；
- 所有开发通过测试后方可合并；
- 完成开发任务后同步更新 `README.md`。

---

## 2. 覆盖原则与范围

## 2.1 覆盖原则

1. **设计全覆盖**：`design-v.1.3.md` 的 `1~14` 章必须有对应开发任务或验收条目；
2. **问题全覆盖**：`analysis-bridge-protocol-v1.md` 的架构/协议/实现/性能/可靠性问题需全部进入修复清单（含“本期实现/本期落设计+验证”标记）；
3. **测试先行**：每个任务必须带测试用例与通过标准；
4. **文档闭环**：代码完成后更新 README（新增能力、API、配置、运行方式、测试命令）。

## 2.2 文档覆盖矩阵（Design v1.3）

| Design 章节 | 开发任务映射 | 交付状态定义 |
|---|---|---|
| `1` 文档目的 | 版本范围与 DoD 固化到本计划 | 必须完成 |
| `2` 现状与问题 | 建立基线指标（时延/带宽/ACK） | 必须完成 |
| `3` 优先级 | P1/P2/P3 排期与资源分配 | 必须完成 |
| `4` 异步关联响应 | `SendSyncAsync` + 兼容层 + 响应关联订阅 | 必须完成 |
| `5` 字段裁剪/压缩/TopicAlias | 方向化序列化 + gzip/zstd + MQTT5 评估实现 | 本期完成前两项；Alias 至少 PoC |
| `6` 智能混合路由 | 网络域模型 + 路由决策器 + 回退机制 | 本期完成骨架+灰度 |
| `7` QoS 分层 | 消息类型 QoS 策略与配置化 | 必须完成 |
| `8` 分阶段计划 | 转为可执行 Sprint/Wave 任务 | 必须完成 |
| `9` SLO | 指标埋点与验收阈值落地 | 必须完成 |
| `10` 总结 | 转化为发布说明草稿 | 必须完成 |
| `11` 与 v1.0 对照 | 迁移检查单与兼容说明 | 必须完成 |
| `12` DdMessage 优化 | `meta/protocol_meta` 模型与迁移实现 | 必须完成 |
| `13` DdPeer 声明式注册 | `request_meta_schema` 存储/校验链路 | 必须完成 |
| `14` main.go 协议资源入口 | HTTP/MQTT/CoAP 协议资源 API | 必须完成 |

## 2.3 文档覆盖矩阵（Analysis 报告）

> 说明：所有条目均纳入 v1.3；若无法在本期一次性完成，至少交付“可运行骨架 + 测试 + 风险控制 + 下一步计划”。

| Analysis 章节 | 问题摘要 | v1.3 对应任务 |
|---|---|---|
| `1.1` | Broker 单点 | 集群化留作后续，先加降级策略与健康告警 |
| `1.2` | Hub-and-Spoke 瓶颈 | 异步关联响应 + 同域直连旁路 |
| `1.3` | Bridge 生命周期缺失 | Bridge 增加 `Stop/Shutdown` 接口与 main 优雅退出 |
| `1.4` | Stream 与 Bridge 割裂 | Stream 控制面与资源注册联动，统一协议元数据 |
| `2.1` | Resource/PeerId 语义混淆 | 资源寻址模型修订 + topic 语义拆分 |
| `2.2` | 无版本号 | 信封增加 `version` 字段（兼容默认） |
| `2.3` | Payload 不透明 | `protocol_meta` + content-type/encoding 标准化 |
| `2.4` | 双 Header 混乱 | `meta/protocol_meta` 重构 + 迁移 |
| `2.5` | 缺 QoS 分级 | QoS 分层策略配置化 |
| `2.6` | reply topic 硬编码 | 统一 tenant-aware reply topic 生成器 |
| `3.1` | CoAP 超时硬编码 | 改用 `timeout_ms` |
| `3.2` | CoAP 每次建连 | UDP 连接池化 |
| `3.3` | CoAP msg id 碰撞 | 原子序列或安全随机 |
| `3.4` | CoAP UTF-8 option bug | 按字节长度正确编码 |
| `3.5` | CoAP option length 解析错误 | 按协议修复 parser |
| `3.6` | HTTP 状态码丢失 | 响应 envelope 保留 status |
| `3.7` | 无重试退避 | HTTP/CoAP 可配置重试与退避 |
| `3.8` | MQTT bridge 忽略 ctx | 传递调用 ctx |
| `3.9` | 幂等淘汰粗糙 | LRU/TTL 语义化改造 |
| `3.10` | transfer status 泄漏 | TTL 清理与上限控制 |
| `3.11` | bridge 不校验消息 | bridge 入口统一 `Validate` |
| `3.12` | stream session 不清理 | main 启动定时 SweepIdle |
| `3.13` | sync response 竞态 | pending channel 投递机制修复 |
| `4.1` | JSON CPU 热点 | 序列化抽象层 + 二进制预研 |
| `4.2` | sync 4 跳延迟 | 异步关联响应 |
| `4.3` | CoAP 串行阻塞 | worker pool/并发执行模型 |
| `4.4` | 全局锁竞争 | 细粒度锁/分片 map |
| `4.5` | HTTP 连接池默认 | transport 参数优化 |
| `4.6` | 无批处理 | 批量发布接口 PoC |
| `4.7` | 无反压 | broker 堆积监测 + 限流策略 |
| `4.8` | CoAP MTU 截断 | block-wise 或分片方案 |
| `5.1~5.8` | 带宽路径与建议 | 字段裁剪、压缩、QoS、TopicAlias、旁路路由 |
| `6.1` | 无 DLQ | 接入 MQ 原生 DLQ 配置与消费 |
| `6.2` | 无熔断 | 下游桥接熔断器 |
| `6.3` | bridge 可观测性缺失 | bridge 指标/日志/trace |
| `6.4` | 无 TLS/mTLS | 连接配置与证书加载 |
| `6.5` | trace_id 未闭环 | trace_id 生成与透传 |
| `7` | 改进建议摘要 | 全量映射到本计划 Wave |

---

## 3. 开发波次（Waves）

## 3.1 Wave 0：基线与安全网（必须先做）

目标：建立可比较基线，保证后续重构有回归保护。

任务：

1. 建立性能与带宽基线脚本（sync/async/heartbeat）；
2. 补齐关键回归测试：
   - `DdDataService` sync/async；
   - HTTP/CoAP bridge；
   - peer registry/resource catalog；
3. 建立统一测试入口（`make test`, `make test-integration`, `make bench`）。

验收：

- baseline 数据可重复；
- CI 绿线稳定。

## 3.2 Wave 1：P1 核心链路（异步关联 + 信封瘦身）

任务：

1. `SendSyncAsync` 实现与 `SendSync` 兼容封装；
2. 响应关联模型：
   - `response/{request_id}` 专属 topic 或统一响应流 + correlation 匹配；
3. 字段方向化裁剪（upstream/downstream）；
4. 全信封压缩（gzip 默认，zstd 可配）；
5. 修复 `3.13` 竞态、`3.11` 校验缺失。

测试：

1. 单测：关联匹配、超时回收、取消传播、裁剪正确性、压缩兼容；
2. 集成：异步关联端到端；
3. 压测：p95 延迟下降验证。

## 3.3 Wave 2：DdMessage 模型重构（Design 12）

任务：

1. 引入 `meta` / `protocol_meta`；
2. 协议白名单校验器（HTTP/MQTT/CoAP/Stream）；
3. 旧 `Headers` 三阶段迁移（双写兼容 -> 灰度 -> 收敛）；
4. `trace_id` 透传骨架与可观测埋点。

测试：

1. 模型编解码兼容测试（新老消息互通）；
2. 协议字段非法输入拒绝测试；
3. 回归 bridge 读写路径测试。

## 3.4 Wave 3：DdPeer 声明式资源元数据（Design 13）

任务：

1. 扩展 `DdPeer`/resource descriptor：
   - `request_meta_schema`
   - `protocol_meta_required`
2. 扩展 `PeerResourceReport` 与 registry 存储结构；
3. API/Bridge 请求入口做 schema 校验（宽松/严格模式）；
4. capability 开关：`resource_meta_schema_v1`。

测试：

1. schema 注册与查询测试；
2. 严格模式拒绝无效请求；
3. 宽松模式兼容旧 peer。

## 3.5 Wave 4：main/API 协议资源入口（Design 14）

任务：

1. 新增协议资源索引服务；
2. 新增 HTTP API：
   - `GET /dd/api/v1/protocol-resources/http`
   - `GET /dd/api/v1/protocol-resources/http/{resource}`
   - `GET /dd/api/v1/protocol-resources/mqtt/topics`
   - `GET /dd/api/v1/protocol-resources/mqtt/topics/{name}`
   - `GET /dd/api/v1/protocol-resources/coap`
   - `GET /dd/api/v1/protocol-resources/coap/{resource}`
3. main 注入索引依赖并启动；
4. 与旧 `/resources*` 接口并存。

测试：

1. API handler 单测（过滤参数、分页、错误码）；
2. 端到端测试（注册资源 -> 查询协议视图）。

## 3.6 Wave 5：可靠性与性能缺口补齐（Analysis 3/4/6）

任务（按优先级）：

1. CoAP 修复组：`3.1~3.5`、`4.8`；
2. HTTP/MQTT 修复组：`3.6~3.8`、`4.5`；
3. 服务稳定性组：`3.9~3.12`、`4.4`、`4.7`；
4. 可观测与安全组：`6.1~6.5`；
5. Topic Alias 与批处理 PoC：`5.8`、`4.6`。

测试：

1. 故障注入（超时、丢包、连接中断、重试风暴）；
2. soak test（长时间运行内存稳定性）；
3. 安全回归（TLS/mTLS 配置正确性）。

---

## 4. 测试与质量门禁（强制）

## 4.1 必须通过的测试层级

1. **Unit**：所有新增/修改函数必须有单测；
2. **Integration**：跨模块链路必须有集成测试；
3. **E2E**：核心路径（sync/async/resource query）端到端可验证；
4. **Performance**：至少对比 v1.2 基线，验证 SLO 方向改进；
5. **Fault Injection**：关键失败模式必须覆盖。

## 4.2 PR 合并门禁

每个 PR 必须满足：

1. `go test ./...` 全通过；
2. 新增测试覆盖对应变更点（禁止无测功能 PR）；
3. 关键路径改动附性能对比（至少 1 组基线对照）；
4. 文档同步（design/plan/README 对应章节）。

---

## 5. README 同步策略（强制）

完成每个 Wave 后必须更新 `README.md`，至少包含：

1. 新增 API 列表与示例请求；
2. 配置项与默认值变更；
3. 新增能力开关（如 schema 严格模式、压缩、QoS 分层）；
4. 测试命令与验证步骤；
5. 兼容性说明（新旧消息格式互通策略）。

发布前检查：

- README 与实际路由、配置、命令一致；
- 删除过时字段说明（尤其 `Headers` 旧行为）。

---

## 6. 里程碑与退出标准

| 里程碑 | 条件 |
|---|---|
| M1（Wave1 完成） | Sync 异步关联上线，字段裁剪+压缩可用，核心回归全绿 |
| M2（Wave2/3 完成） | 新信封模型和声明式 schema 上线，兼容迁移可运行 |
| M3（Wave4 完成） | HTTP/MQTT/CoAP 协议资源 API 可查询可验证 |
| M4（Wave5 完成） | Analysis 关键缺陷修复闭环，SLO 达标，README 完整 |

版本退出标准（v1.3 GA）：

1. `design-v.1.3.md` 与 `analysis-bridge-protocol-v1.md` 覆盖清单均勾选完成；
2. 测试门禁全通过；
3. README 更新完成并经人工核验。

---

## 7. 任务拆解建议（执行顺序）

1. 先做 Wave0 + Wave1（打穿主链路）；
2. 再做 Wave2 + Wave3（模型与治理）；
3. 然后 Wave4（入口能力）；
4. 最后 Wave5（缺口补齐与硬化）。

建议 PR 粒度：

1. 每个 PR 只覆盖一个子能力（如“SendSyncAsync + tests”）；
2. 大改分“模型层 PR -> 服务层 PR -> API 层 PR”；
3. 每个 PR 附“影响矩阵（设计章节/分析条目）”。

---

## 8. 开发启动 Prompt（可直接用于执行）

请基于 `docs/design-v.1.3.md` 与 `docs/analysis-bridge-protocol-v1.md` 启动 v1.3 开发，严格按以下规则执行：

1. 全覆盖要求  
   - 逐条建立任务映射，覆盖 design 的全部章节（`1~14`）与 analysis 的全部问题条目（`1.x~7`）。  
   - 每完成一个任务，更新覆盖清单状态（done/in-progress/todo）。

2. 测试要求（强制）  
   - 所有开发必须包含测试；无测试的功能不允许合并。  
   - 至少执行：`go test ./...`，并补充必要的集成/E2E/性能对比测试。  
   - 对关键路径（sync/async/bridge/resource query）提供回归验证结果。

3. 文档要求（强制）  
   - 每个功能完成后同步更新相关设计/计划条目。  
   - 在全部开发任务完成时，必须同步更新 `README.md`：新增能力、API、配置、测试命令、兼容性说明。  
   - README 必须与代码实现保持一致。

4. 交付要求  
   - 输出每个 Wave 的完成报告：变更文件、测试结果、性能变化、风险与回滚方案。  
   - 最终给出 v1.3 发布前检查结果（覆盖率、测试、README 三项全部通过）。
