# DD Core PRD v1 - MQ Basedata distribution service

## Main scope

DD is a message queue based data distribution service. The core functions include
1. Protocol transfer: pass different protocols including http, rpc, socket, coap, etc., into binary streams over the MQ and reconstruct it from the messages.
2. Connection handshake and keepalive handling: using constraints based topic subscrption to trace the socket, session, and other connections.
3. Sync and async transfer: for sync connections, using extra goroutines to manage the connection via timeout, for async connections, use timeout flag emitting to control the lifecycle of the connection.
4. Distributed DdPeer discovery: using constraint based topic pattern to for resource discovery including data, state, and other system resources

## Project structure
Using typical golang project structure as:
```bash
-- cmd //main.go and other program entrances
-- internal //core libraries
-- api // web based api for
-- docs //output of promppts, plans, spec, summaries, etc.
```