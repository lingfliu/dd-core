# MemMQ PRD v1 - Non-persistent Ultra-high Performance Message Queue

## 1. Background and objective

The DD framework needs a dedicated in-memory message queue that prioritizes throughput and tail-latency over durability.  
This queue must support long-lived message streaming over both TCP and KCP for low-latency, high-concurrency internal data distribution.

`MemMQ` is designed as a **non-persistent**, **single-hop, memory-resident** broker optimized for:
- ultra-low enqueue/dequeue latency;
- high fan-in/fan-out streaming;
- protocol-level support for TCP and KCP transports;
- direct protocol passthrough streaming for media/control streams (for example RTSP in/out);
- predictable behavior under overload through explicit backpressure and drop policy.

## 2. Goals and non-goals

### 2.1 Goals (v1)
1. Provide a memory-only MQ core with topic-based pub/sub.
2. Support transport servers for both TCP and KCP.
3. Support streaming message frames for high-frequency producers/consumers.
4. Provide explicit QoS modes for non-persistent delivery.
5. Expose management APIs and runtime metrics.
6. Enforce topic-level access control for publish/subscribe operations.
7. Achieve benchmarkable high-performance targets on single node.

### 2.2 Non-goals (v1)
1. Disk persistence, WAL, or replay after restart.
2. Cross-node consensus and strongly consistent replication.
3. Exactly-once delivery guarantees.
4. Multi-tenant billing and advanced policy engine.

## 3. Users and use cases

### 3.1 Users
- DD platform components requiring a local/cluster-edge transient queue.
- High-frequency data producers (telemetry, sensor, stream transformers).
- Real-time consumers requiring low end-to-end delay.

### 3.2 Core use cases
1. Producer streams telemetry at high frequency over KCP to MemMQ.
2. Multiple consumers subscribe by topic and receive near-real-time messages.
3. System falls behind under burst load and applies configured backpressure/drop behavior.
4. Operators inspect queue depth, consumer lag, and per-transport health.

## 4. Product scope and architecture

### 4.1 Core modules
1. **Broker Core (in-memory)**
   - Topic registry, subscriber registry, queue shards, scheduler.
2. **Transport Layer**
   - TCP server (streaming frames).
   - KCP server (session-oriented reliable UDP stream).
3. **Protocol Layer**
   - Binary frame format, handshake/auth, heartbeat, ack/nack, flow-control signals.
4. **Dispatch Engine**
   - Lock-minimized fan-out and batched delivery.
5. **Backpressure Controller**
   - Per-topic/per-subscriber queue limits and overload policy.
6. **Passthrough Stream Engine**
   - Direct byte-stream forwarding pipeline for protocol-preserving streaming (no message slicing).
7. **Access Control Engine**
   - Topic ACL policy evaluation and authz cache.
8. **Management Plane**
   - HTTP admin APIs and Prometheus-like metrics endpoint.

### 4.2 High-level flow
1. Producer connects via TCP or KCP and sends `PUBLISH` stream frames.
2. Broker validates frame and routes to topic shard ring buffer.
3. Dispatch engine pushes message frames to subscriber streams.
4. Consumer ack policy updates in-memory delivery state (optional by QoS mode).
5. Expired/dropped frames are counted and exposed in metrics.

### 4.3 Distribution modes
1. **Message mode (default)**
   - Producer sends framed `PUBLISH` messages.
   - Broker routes discrete messages by topic and QoS settings.
2. **Direct streaming mode (passthrough)**
   - Producer opens a topic-bound stream tunnel (for example RTSP over TCP/KCP).
   - Broker does not parse or slice media payload into message units.
   - Broker forwards byte stream chunks to authorized subscribers preserving stream semantics.
   - Optional lightweight framing is transport-level only (length-prefixed chunks), not application message conversion.

## 5. Functional requirements

### FR-1 Topic and subscription
1. Broker MUST support create-on-publish topic registration.
2. Broker MUST support direct topic subscription.
3. Broker SHOULD support wildcard subscription (`*` and `>` style) in v1.1 (optional in v1).

### FR-2 Stream publish and consume
1. Client MUST be able to open one connection and multiplex multiple topics.
2. Server MUST support message framing with zero-copy friendly payload handling.
3. Server MUST support batch publish frame for reduced syscall overhead.

### FR-3 QoS and delivery semantics
1. `QoS0` (default): at-most-once, no ack.
2. `QoS1`: at-least-once in-memory session ack with redelivery during connection lifetime.
3. On process restart, unacked messages are dropped by design.

### FR-4 Flow control and overload behavior
1. Per-subscriber outbound buffer limit MUST be configurable.
2. On overload, one of policies MUST apply:
   - `drop_oldest`,
   - `drop_latest`,
   - `disconnect_slow_consumer`,
   - `block_producer` (bounded wait timeout).
3. System MUST expose drop/block/disconnect counters.

### FR-5 KCP and TCP support
1. Broker MUST provide both TCP and KCP listeners.
2. KCP profile SHOULD be configurable (`nodelay`, `interval`, `resend`, `nc`, send/recv window).
3. TCP profile SHOULD support `TCP_NODELAY`, keepalive, read/write buffer tuning.

### FR-6 Direct streaming passthrough mode
1. Broker MUST support opening long-lived stream channels bound to a topic.
2. In passthrough mode, broker MUST NOT transform payload into logical message slices.
3. Broker MUST support one-to-many stream fan-out from one upstream source to multiple downstream subscribers.
4. Stream lifecycle MUST include open, active, half-close, full-close, timeout, and error termination states.
5. Stream mode SHOULD support backpressure propagation to upstream source (bounded buffers + flow-control signal).

### FR-7 Topic access control
1. Broker MUST authorize `publish`, `subscribe`, and `stream_open` actions per topic.
2. Policy model MUST support allow/deny by:
   - `client_id`,
   - `role`,
   - `namespace/topic pattern`,
   - `action`.
3. Deny rules MUST take precedence over allow rules.
4. Authz decision SHOULD be cached with TTL and invalidated on policy change.
5. Unauthorized operations MUST return explicit error code and be audit-logged.

### FR-8 Health and lifecycle
1. Clients MUST complete handshake before publish/subscribe.
2. Heartbeat MUST evict stale sessions.
3. Broker MUST provide graceful shutdown with drain timeout.

## 6. Data model and protocol contract

### 6.1 In-memory entities
- `ClientSession`
  - `session_id`, `client_id`, `transport`, `state`, `last_seen_at`
- `TopicShard`
  - `topic`, `shard_id`, `ring_buffer`, `depth`, `drop_count`
- `SubscriberCursor`
  - `session_id`, `topic`, `read_offset`, `lag`, `qos`

### 6.2 Binary frame format (v1)
```text
+---------+---------+-----------+-------------+-----------+----------+
| magic   | version | frameType | flags       | streamId  | length   |
| 2 bytes | 1 byte  | 1 byte    | 2 bytes     | 4 bytes   | 4 bytes  |
+---------+---------+-----------+-------------+-----------+----------+
| payload (length bytes)                                             |
+--------------------------------------------------------------------+
```

Frame types:
- `0x01` CONNECT
- `0x02` CONNACK
- `0x03` PUBLISH
- `0x04` SUBSCRIBE
- `0x05` SUBACK
- `0x06` MESSAGE
- `0x07` ACK
- `0x08` NACK
- `0x09` PING
- `0x0A` PONG
- `0x0B` ERROR
- `0x0C` STREAM_OPEN
- `0x0D` STREAM_DATA
- `0x0E` STREAM_CLOSE

### 6.3 Publish payload (logical fields)
```json
{
  "topic": "dd.stream.sensor.temp",
  "message_id": "uint64-or-uuid",
  "timestamp_unix_nano": 0,
  "qos": 0,
  "ttl_ms": 0,
  "headers": {"trace_id": "abc"},
  "body": "bytes"
}
```

### 6.4 Topic naming
```text
memmq.v1.{namespace}.{domain}.{resource}
```
Examples:
- `memmq.v1.default.telemetry.gps`
- `memmq.v1.default.events.alert`

## 7. APIs

## 7.1 Client stream APIs (protocol-level)
1. `CONNECT(client_id, auth, keepalive_ms) -> CONNACK`
2. `SUBSCRIBE(topic, qos) -> SUBACK`
3. `PUBLISH(topic, body, qos, ttl_ms) -> [ACK if qos1]`
4. `PING -> PONG`
5. `STREAM_OPEN(topic, mode=passthrough, metadata) -> STREAM_ACK/ERROR`
6. `STREAM_DATA(stream_id, chunk_bytes) -> [optional WINDOW_UPDATE/ERROR]`
7. `STREAM_CLOSE(stream_id)`
8. `UNSUBSCRIBE(topic)` (optional in v1, recommended)
9. `DISCONNECT`

### 7.2 Admin HTTP APIs
Base path: `/memmq/api/v1`

1. `GET /health`
   - Liveness/readiness.
2. `GET /stats`
   - Node throughput, p50/p95/p99 latency, drops, active sessions.
3. `GET /topics`
   - Topic list and depth.
4. `GET /topics/{topic}`
   - Topic shard details and lag summary.
5. `POST /topics/{topic}/purge`
   - Drop all buffered messages for topic.
6. `GET /sessions`
   - Active session list with transport and lag.
7. `DELETE /sessions/{sessionId}`
   - Force disconnect.
8. `GET /config`
   - Runtime effective config.
9. `POST /config/reload`
   - Reload dynamic config subset.
10. `GET /acl/policies`
   - List topic ACL policies.
11. `POST /acl/policies`
   - Create or update ACL policy.
12. `DELETE /acl/policies/{policyId}`
   - Remove ACL policy.
13. `POST /acl/reload`
   - Refresh ACL cache immediately.

## 8. Configuration requirements

Mandatory config domains:
1. Listener config:
   - `tcp.listen_addr`, `kcp.listen_addr`.
2. Queue config:
   - `topic_shards`, `ring_capacity`, `max_topic_count`.
3. Flow control:
   - `max_inflight_per_sub`, `overload_policy`, `producer_block_timeout_ms`.
4. Session:
   - `handshake_timeout_ms`, `heartbeat_timeout_ms`, `max_connections`.
5. Stream mode:
   - `stream_chunk_size`, `stream_idle_timeout_ms`, `stream_max_fanout`, `stream_buffer_bytes`.
6. ACL:
   - `acl_enabled`, `acl_policy_source`, `acl_cache_ttl_ms`, `acl_default_action`.
7. KCP tuning:
   - `kcp_nodelay`, `kcp_interval`, `kcp_resend`, `kcp_nc`, `kcp_sndwnd`, `kcp_rcvwnd`.
8. TCP tuning:
   - `tcp_nodelay`, `tcp_keepalive_sec`, socket read/write buffers.

## 9. Non-functional requirements

### 9.1 Performance targets (single node baseline)
1. Throughput:
   - >= 1,000,000 msgs/sec for small payload (<= 128 bytes), QoS0, in-memory.
2. Publish latency:
   - p95 <= 1 ms, p99 <= 3 ms under baseline load.
3. End-to-end delivery latency:
   - p95 <= 5 ms for single publisher-single subscriber path.

### 9.2 Reliability and availability
1. Process crash or restart leads to full in-memory loss (expected behavior).
2. During runtime, broker SHOULD avoid global lock contention and stop-the-world hotspots.
3. Graceful shutdown MUST provide bounded drain window.

### 9.3 Security
1. Minimum auth: client key/token at handshake.
2. Optional TLS for TCP and DTLS-like tunnel strategy for KCP (v1 optional).
3. Topic ACL MUST enforce action-level authorization (`publish`, `subscribe`, `stream_open`).
4. Admin API MUST support authn/authz and rate limiting.

### 9.4 Observability
1. Metrics: enqueue rate, dequeue rate, drop count, lag, active sessions, per-transport errors.
2. Logs: structured logs with connection id, topic, qos, error code.
3. Tracing: optional trace propagation via message headers.

## 10. Error model

Common error codes:
- `MEMMQ-4001` invalid frame
- `MEMMQ-4002` unauthorized
- `MEMMQ-4003` topic invalid
- `MEMMQ-4004` qos unsupported
- `MEMMQ-4011` acl deny
- `MEMMQ-4092` stream not found or closed
- `MEMMQ-4091` backpressure reject
- `MEMMQ-4291` too many connections
- `MEMMQ-5001` internal dispatch error

## 11. Milestones

1. **M1 - Core engine**
   - topic shard, ring buffer, dispatch loop, QoS0.
2. **M2 - Transport**
   - TCP + KCP listeners, handshake, heartbeat.
3. **M3 - QoS and control**
   - QoS1 ack path, backpressure policy, admin APIs, ACL v1.
4. **M4 - Perf and hardening**
   - passthrough stream mode, benchmark suite, profiling, lock/contention optimization, observability completion.

## 12. Acceptance criteria (v1)

1. Producer and consumer can stream over both TCP and KCP.
2. Topic pub/sub supports sustained high load with bounded latency.
3. Direct streaming passthrough mode forwards RTSP-like streams without message slicing conversion.
4. Topic ACL blocks unauthorized publish/subscribe/stream actions and records audit events.
5. Overload policy works as configured and emits accurate counters.
6. Admin API exposes live health, stats, topics, sessions, and ACL policy state.
7. Benchmarks demonstrate target class performance on reference hardware.
