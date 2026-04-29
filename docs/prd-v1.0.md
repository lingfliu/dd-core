# DD Core PRD v1.1 - MQ Based Data Distribution Framework

## 1. Background and problem statement

Distributed systems often need to exchange data across different protocols (HTTP, RPC, socket, CoAP, and custom binary).  
Point-to-point integration introduces coupling, difficult routing changes, and weak observability.

`dd-core` defines an MQ-based data distribution framework that:
- normalizes protocol data into a unified envelope;
- routes by topic and constraints;
- tracks peer and session lifecycle;
- supports sync, async, and stream transfer modes;
- exposes control APIs for discovery and operations.

## 2. Goals and non-goals

### 2.1 Goals (v1.1)
1. Define a stable message envelope and topic convention.
2. Support protocol adapter flow: ingress protocol -> MQ envelope -> egress protocol.
3. Provide peer registration, heartbeat, and discovery.
4. Support sync request-response, async event delivery, and stream passthrough mode.
5. Provide HTTP control APIs for peer/resource/transfer/connection operations.
6. Provide baseline security, observability, and error model.

### 2.2 Non-goals (v1.1)
1. Global multi-region consistency and smart geo-routing.
2. Exactly-once delivery across all adapters.
3. Full policy governance and enterprise RBAC model.
4. Durable business payload storage in the DD core itself.

## 3. Users and use cases

### 3.1 Users
- Platform engineers integrating protocol adapters.
- Service developers publishing and consuming resources.
- SRE/Ops engineers monitoring transfer health and routing status.

### 3.2 Core use cases
1. Convert protocol-specific input to MQ messages and deliver to multiple subscribers.
2. Execute synchronous request-response transfer with timeout and correlation tracking.
3. Run low-overhead stream distribution using minimal metadata envelope profile.
4. Keep peers discoverable through registration and heartbeat.
5. Query active sessions, routes, and transfer status for debugging.

## 4. Product scope and architecture

### 4.1 Core modules
1. **Protocol Adapter Layer**
   - Inbound adapters: HTTP, RPC, socket, CoAP.
   - Outbound adapters: rebuild target protocol payload from DD envelope.
2. **Envelope and Codec**
   - Unified schema, binary payload handling, compression flag, optional signature.
3. **Router**
   - Topic-based routing by tenant/resource/peer/session constraints.
4. **Peer Registry**
   - Register, heartbeat lease, status transition, capability tagging.
5. **Transfer Manager**
   - Sync lifecycle (timeout, correlation, retry).
   - Async lifecycle (TTL, retry, dead-letter).
   - Stream lifecycle (session, flow-control, graceful close).
6. **Control Plane API**
   - Management APIs for discovery, transfer, and connection queries.
7. **Observability**
   - Metrics, structured logs, and trace propagation.

### 4.2 High-level flow
1. Producer request enters an ingress adapter.
2. Adapter creates `DdMessage` envelope and publishes to MQ topic.
3. Router and consumers subscribe by constraints and receive the message.
4. Egress adapter reconstructs target protocol payload.
5. For sync mode, response is published to correlation response topic.
6. For stream mode, producer and subscribers exchange long-lived low-metadata envelopes.

## 5. Functional requirements

### FR-1 Protocol transfer
1. System MUST support binary-safe payload forwarding.
2. System MUST include `protocol`, `content_type`, and `encoding` metadata.
3. System SHOULD support payload chunking for large messages in a future version.

### FR-2 Connection handshake and keepalive
1. Peer MUST register before service traffic.
2. Heartbeat interval and lease timeout MUST be configurable.
3. Peer status transitions MUST include:
   - `registering` -> `active` -> `stale` -> `offline`.

### FR-3 Sync, async, and stream transfer
1. Sync mode MUST support:
   - `request_id`/`correlation_id`,
   - configurable timeout,
   - response topic routing.
2. Async mode MUST support:
   - ttl/expiry,
   - at-least-once target behavior,
   - dead-letter topic after retry exhaustion.
3. Stream mode MUST support:
   - long-lived transfer with stable `stream_id`,
   - minimal metadata envelope profile,
   - zero-metadata payload chunks after initial stream handshake,
   - bounded flow-control and idle timeout.

### FR-4 Peer discovery
1. Registry MUST support filters:
   - `role`, `capabilities`, `resource`, `status`.
2. Registry MUST support point lookup by peer id.

### FR-5 Resource request and response
1. Peer MUST be able to register resource descriptors.
2. Requester MUST be able to query resource providers.
3. Transfer API MUST return route metadata and final transfer status.

### FR-6 Connection and route query
1. Control API MUST list active connections and sessions.
2. Control API MUST provide effective route/subscription view.

## 6. Data model and contracts

### 6.1 Peer model (`DdPeerInfo`)
Required fields:
- `id`, `name`, `role`, `key`, `secret`, `created_at`, `updated_at`

Recommended runtime fields in v1.1:
- `status`: `registering|active|stale|offline`
- `last_heartbeat_at`
- `capabilities: []string`
- `tags: map[string]string`

### 6.2 Message envelope (`DdMessage`)
```json
{
  "message_id": "uuid",
  "request_id": "uuid-optional",
  "correlation_id": "uuid-optional",
  "mode": "sync|async|stream",
  "envelope_profile": "full|minimal|zero",
  "source_peer_id": "peer-a",
  "target_peer_id": "peer-b-optional",
  "resource": "sensor.temp",
  "protocol": "http|rpc|socket|coap|custom",
  "content_type": "application/json",
  "encoding": "raw|gzip|snappy",
  "timestamp": "2026-04-26T16:00:00Z",
  "ttl_ms": 10000,
  "headers": {"trace_id": "xxx"},
  "payload": "base64-bytes",
  "signature": "optional-signature"
}
```

### 6.3 Envelope profiles for stream mode
1. `full` (default for sync/async)
   - Uses complete metadata fields in `DdMessage`.
2. `minimal` (recommended for stream control and keyframes)
   - Required fields: `mode`, `stream_id`, `resource`, `payload`.
   - Optional fields: `timestamp`, `headers`, `encoding`.
3. `zero` (recommended for stream data chunks)
   - No per-chunk business metadata in payload envelope body.
   - Only transport/session framing carries routing context from stream handshake.

Minimal stream envelope example:
```json
{
  "mode": "stream",
  "envelope_profile": "minimal",
  "stream_id": "strm-1001",
  "resource": "video.rtsp.camera-1",
  "payload": "base64-bytes"
}
```

Zero-metadata stream chunk example (logical):
```json
{
  "mode": "stream",
  "envelope_profile": "zero",
  "stream_id": "strm-1001",
  "payload": "base64-bytes"
}
```

### 6.4 Topic naming convention
```text
dd.v1.{tenant}.{domain}.{resource}.{action}
```
Examples:
- `dd.v1.default.peer.registry.register`
- `dd.v1.default.peer.registry.heartbeat`
- `dd.v1.default.transfer.sensor.temp.request`
- `dd.v1.default.transfer.sensor.temp.response`
- `dd.v1.default.transfer.video.rtsp.stream.open`
- `dd.v1.default.transfer.video.rtsp.stream.data`
- `dd.v1.default.transfer.dlq`

## 7. API design

Base path: `/dd/api/v1`

### 7.1 Peer APIs
1. `POST /peers/register`
   - Register peer and return lease metadata.
2. `POST /peers/{id}/heartbeat`
   - Renew lease and update runtime metadata.
3. `POST /peers/{id}/unregister`
   - Graceful offline.
4. `GET /peers/{id}`
   - Query peer details.
5. `GET /peers?role=&status=&capability=&resource=`
   - Filtered peer discovery.

### 7.2 Resource APIs
1. `POST /resources/register`
   - Register resource provider mapping.
2. `DELETE /resources/{resource}/providers/{peerId}`
   - Remove resource-provider mapping.
3. `GET /resources/{resource}/providers`
   - Query provider candidates.
4. `POST /resources/request`
   - Submit resource request with target constraints.
5. `POST /resources/response`
   - Submit resource response payload and status.

### 7.3 Transfer APIs
1. `POST /transfer/sync`
   - Create sync transfer and wait for response or timeout.
2. `POST /transfer/async`
   - Create async event transfer.
3. `POST /transfer/stream/open`
   - Open stream transfer and negotiate envelope profile (`minimal|zero`).
4. `POST /transfer/stream/close`
   - Close stream transfer by `stream_id`.
5. `GET /transfer/{requestId}`
   - Query transfer status and route history.

### 7.4 Connection and route APIs
1. `GET /connections`
   - List active sessions and connections.
2. `GET /connections/{id}`
   - Connection/session detail.
3. `GET /routes`
   - Effective route and subscription view.

## 8. Non-functional requirements

### 8.1 Performance
1. Async publish latency target: p95 < 50 ms (excluding business processing).
2. Sync round-trip target: p95 < 300 ms under baseline load.
3. Throughput target: baseline >= 5k msgs/s per node for standard payload profile.
4. Stream data path SHOULD support lower per-message overhead with `minimal/zero` envelope profile.

### 8.2 Reliability
1. Retry policy MUST be configurable by mode and topic.
2. Async retry exhaustion MUST route to DLQ.
3. Idempotency key SHOULD be supported for sync retries.
4. Stream mode MUST tolerate transient subscriber disconnect with configurable reconnection window.

### 8.3 Security
1. Peer authentication via key/secret or token.
2. Optional message signature verification.
3. Sensitive fields MUST be masked in logs.

### 8.4 Observability
1. Metrics dimensions: topic/resource/mode/status/peer.
2. Trace propagation via `trace_id` in message headers.
3. Structured logs MUST include request id and correlation id for sync flow.
4. Stream mode metrics MUST include `stream_open`, `stream_close`, `stream_drop`, and `stream_profile`.

## 9. Error model and retry strategy

### 9.1 Standard transfer statuses
- `accepted`
- `routed`
- `delivered`
- `responded`
- `timeout`
- `failed`
- `dead_lettered`

### 9.2 Error codes
- `DD-4001` invalid envelope
- `DD-4002` unauthorized peer
- `DD-4041` route not found
- `DD-4081` sync timeout
- `DD-4091` retry exhausted
- `DD-4092` stream not found
- `DD-4093` stream idle timeout
- `DD-5001` internal transfer error

### 9.3 Default retry policy
- Sync: max 1 retry, requires idempotency.
- Async: exponential backoff, max 3 retries, then DLQ.

## 10. Milestones

1. **M1 - Core contract and router**
   - Envelope schema, topic standard, base route pipeline.
2. **M2 - Peer registry**
   - Register/heartbeat/discovery APIs and in-memory registry.
3. **M3 - Transfer manager**
   - Sync/async/stream lifecycle with timeout/retry/query APIs.
4. **M4 - Hardening**
   - Metrics, logs, stream profile optimization, auth baseline, benchmark and docs.

## 11. Acceptance criteria

1. Peer can register and remain active through heartbeat lease renewal.
2. Discovery returns accurate online peers by role/capability/resource filters.
3. Sync transfer returns correlated response before timeout in normal path.
4. Async transfer retries correctly and routes exhausted messages to DLQ.
5. Stream mode works with `minimal` and `zero` envelope profiles for low-overhead distribution.
6. APIs expose reliable connection, route, and transfer status.

## 12. Project structure (reference)

```bash
cmd/        # main entrypoints
internal/   # core domain and service libraries
api/        # HTTP/OpenAPI contracts and handlers
docs/       # PRD, plans, specs, summaries
```
