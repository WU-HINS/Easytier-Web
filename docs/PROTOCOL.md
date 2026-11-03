# EasyTier Web 控制面协议规范（对齐 2.7.0 / commit 24e0303）

本文是重写的**不变量清单**：凡是本文写死的东西，实现必须逐字节一致；本文没写的，
实现可以自由发挥。所有结论均来自对 Rust 源码的逐行阅读，行号可在
`easytier-src` 中复核。

## 0. 一句话结论

控制面协议 = **四层封装**：

```
[传输帧]  TCP: u32 LE 长度前缀 | WS: 一条二进制消息 = 一个包 | UDP: 8 字节头 + SYN/SACK/DATA/FIN
   ↓
[PeerManager 头]  16 字节，含 packet_type
   ↓
[RpcPacket]        protobuf，含 transaction_id / descriptor / 分片字段
   ↓
[RpcRequest | RpcResponse]  protobuf，真正的业务载荷
```

任何一层不一致都会导致设备连不上，而 REST/DB/UI 层怎么改都不影响互通。

---

## 1. 传输帧

### 1.1 TCP（`ET_CONFIG_SERVER_PROTOCOL=tcp`，源码 `.github/workflows` 默认 udp，TCP 最易实现）

```
+---------------------+-------------------------------+---------------------+
| len (u32, LE)       | PeerManager header (16 B)     | payload (N bytes)   |
+---------------------+-------------------------------+---------------------+
len = 16 + N            <- 注意：len 计入了 PeerManager 头，不含自身 4 字节
```

- 实现参考：`easytier-core/src/tunnel/framed.rs` 的 `TcpZCPacketToBytes`（`tcp_len = PEER_MANAGER_HEADER_SIZE + payload_len`）。
- 读取时 `body_len > max_packet_size` 直接断链；`max_packet_size` 默认 `TCP_MTU_BYTES = 2000`。
- `body_len < 16` 视为非法包。

### 1.2 WebSocket（`ws://` / `wss://`）

一条二进制消息 = 一个完整包（PeerManager 头 + payload），**没有**长度前缀，也**没有** TCP 头。
- 发送：`packet.tunnel_payload_bytes()`（`easytier/src/tunnel/websocket.rs` `WebSocketPacketSink::start_send`）。
- 接收：`new_from_buf(bytes, ZCPacketType::DummyTunnel)`，即 payload_offset = 0。
- 非 binary 消息直接判错断链。

### 1.3 UDP（默认协议，**Phase 2 才实现**）

```
+----------------+-----------+---------+---------+------------------+
| conn_id (u32)  | msg_type  | padding | len u16 | payload          |
| LE             | u8        | u8      | LE      | (len bytes)      |
+----------------+-----------+---------+---------+------------------+
msg_type: 1=Syn 2=Sack 3=Data 4=Fin 5=HolePunch 6=V4HolePunch 7=V6HolePunch
```

UDP 不是裸数据报，而是一层自研可靠传输：`easytier-core/src/socket/udp/` 下
`layer.rs(1208) + session.rs(1073) + packet.rs(404) ≈ 2.5k 行`，含 SYN/SACK 建链、
重传、序号、keepalive。**这是整次重写里最贵的一块**，详见重写计划 §8 风险 R2。

---

## 2. PeerManager 头（16 字节，小端）

```
偏移  长度  字段              取值说明
0     4     from_peer_id      u32 LE
4     4     to_peer_id        u32 LE
8     1     packet_type       见 §3
9     1     flags             bit0 ENCRYPTED / bit4 COMPRESSED，控制面通常为 0
10    1     forward_counter   控制面固定写 1
11    1     reserved          固定 0
12    4     len               u32 LE，= payload 长度（不含本头）
```

- 来源：`easytier-core/src/packet/mod.rs` `struct PeerManagerHeader`（`#[repr(C, packed)]`）。
- 填充函数 `fill_peer_manager_hdr` 明确写死：`flags=0, forward_counter=1, reserved=0, len=payload_len`。
- Go 实现：`internal/wire/packet.go`。
- **注意**：接收侧校验以外层帧长度为准，头里的 `len` 字段 Rust 自己也不交叉校验，重写时同样不要因此断链。

## 3. PacketType

| 值 | 名称 | 控制面是否出现 |
|---|---|---|
| 1 | Data | 否 |
| 2 | HandShake | 否 |
| 8 | **RpcReq** | 是（请求方向） |
| 9 | **RpcResp** | 是（响应方向） |
| 13/14/15 | NoiseHandshakeMsg1/2/3 | 仅安全模式 |
| 20/21 | RelayHandshake/Ack | 否 |

## 4. RpcPacket（protobuf，`common.proto`）

| 字段 | 类型 | 语义 |
|---|---|---|
| from_peer | uint32 | 通常 0（控制面不属任何 peer） |
| to_peer | uint32 | 同上 |
| transaction_id | int64 | 请求/响应配对键，单方向自增即可 |
| descriptor | RpcDescriptor | 仅首片携带（未压缩时每片都带） |
| body | bytes | 见 §6 / §7 |
| is_request | bool | true=请求，false=响应 |
| total_pieces | uint32 | 分片总数，单片时为 1 |
| piece_idx | uint32 | 从 0 开始 |
| trace_id | int32 | 透传 |
| compression_info | RpcCompressionInfo | 见 §8 |

RpcDescriptor：

```
domain_name   允许同一 service 在不同 domain 重复注册（TcpProxy 用）
proto_name    包名，如 "api.manage"
service_name  服务名，如 "WebClientService"
method_index  1 基、按 .proto 中声明顺序
```

## 5. 方法寻址（最容易埋雷的一处）

proto_name/service_name/method_index 三元组由 **ServiceRegistry** 直接哈希匹配：
`easytier-core/src/rpc/service_registry.rs` `ServiceKey{domain_name, service_name, proto_name}`。

`method_index = 方法在 service 块内的声明序号（从 1 开始）`，
生成规则见 `easytier-proto/build/rpc.rs`：`.enumerate().map(|(i, m)| (i + 1) as u8)`。

推论（必须写进 code review 清单）：
1. **在 service 中间插入一个方法 = 破坏所有旧客户端**；只能追加到末尾。
2. 重写实现不能"按字母序/按接口顺序"猜索引，必须从 .proto 生成。

本仓库的解法：`tools/gen_methodindex.py` 解析 .proto 生成
`internal/pb/methodindex/methodindex.go`（当前 21 个服务 / 56 个方法），
并把生成结果入库、CI 比对，任何 proto 顺序变化都会让 diff 现形。

## 6. 请求载荷

`RpcPacket.body`（解压后）= `common.RpcRequest`：

```
message RpcRequest {
  RpcDescriptor descriptor = 1 [deprecated = true];  // 不要填
  bytes request = 2;                                  // 具体方法入参的序列化
  int32 timeout_ms = 3;
}
```

依据：`easytier-core/src/rpc/server.rs` `handle_rpc_request` 里
`RpcRequest::decode(body)` 之后才 dispatch。

## 7. 响应载荷

`RpcPacket.body`（解压后）= `common.RpcResponse`：

```
message RpcResponse {
  bytes response = 1;   // 方法出参的序列化
  error.Error error = 2; // 出错时只填这里
  uint64 runtime_us = 3;
}
```

错误类型是 protobuf oneof（`error.proto`）：
`OtherError / InvalidMethodIndex / InvalidService / ProstDecodeError / ProstEncodeError / ExecuteError / MalformatRpcPacket / Timeout`。

**生成代码命名坑**：Go 里 oneof 包装类型是 `Error_OtherError`（没有末尾下划线），
不是常见的 `Error_OtherError_`，因为包装名与 message 名不冲突。

## 8. 压缩

`RpcCompressionInfo{algo, accepted_algo}`，枚举 `Invalid=0 / None=1 / Zstd=2`。

- `algo`：**本包 body** 用的算法。
- `accepted_algo`：**我方能解**的算法，供对端决定如何回包。
- 压缩只在 `compressed.len() < raw.len()` 时启用，否则回落到 None（`compress_packet`）。
- 未协商时 Rust 侧 `unwrap_or_default()` 得到 0(Invalid)，解码实现必须把 0 与 1 等价处理。

## 9. 分片

- 预算 `RPC_PACKET_UDP_PAYLOAD_BUDGET = 1300` 字节（含隧道头预留），因此 TCP/WS 上也一样分片，单包永不超过 2000 字节。
- 首片（piece_idx=0）携带 descriptor 与 compression_info；未压缩时**每片**都带 descriptor。
- 合并规则：按 transaction_id 聚合，要求 `total_pieces <= 32768`，拼 body 后**以首片为模板**（保留其 descriptor 与 compression_info）。
  这一步做错的典型症状是"大配置推送失败 + 对端报 wire-format 错"，参见本仓库 mux 的回归测试。
- 半成品保留 10 秒后清理。

## 10. UUID 编码（`common.UUID`）

```
message UUID { uint32 part1 = 1; uint32 part2 = 2; uint32 part3 = 3; uint32 part4 = 4; }
```

`part1 = u32::from_be_bytes(uuid_bytes[0..4])`，依此类推（`easytier-proto/src/common.rs:165`）。
**不是** 16 字节 buffer，也**不是** little-endian。machine_id / inst_id 全部走这条编码，
写反了的症状是设备永远"未注册"或串到别人的机器上。

## 11. WebServerService（设备 → 控制台方向）

`web.proto`，服务全名 `web.WebServerService`：

| index | 方法 | 入参 | 出参 |
|---|---|---|---|
| 1 | Heartbeat | HeartbeatRequest | HeartbeatResponse |
| 2 | GetFeature | GetFeatureRequest | GetFeatureResponse |

HeartbeatRequest 关键字段：`user_token`（= 用户名）、`machine_id`、`inst_id`、
`easytier_version`、`report_time`、`hostname`、`running_network_instances`、
`device_os`、`support_config_source`、`failed_network_instances`、
`support_heartbeat_policy`、`support_manual_routes_patch`。

HeartbeatResponse 只有两个可选字段：`heartbeat_interval_ms` / `heartbeat_timeout_ms`。

服务端约束（`client_manager/mod.rs` `HeartbeatPolicy::from_millis`）：

- interval ∈ [1000, 60000] ms，默认 3500
- timeout ∈ [5000, 120000] ms，默认 15000
- **timeout ≥ interval + 5000 ms**，否则启动即失败
- 老客户端（`support_heartbeat_policy=false`）需要"延迟响应"来限速，默认延迟 = min(interval, 3500)

身份绑定：首包心跳把 `(user_token, machine_id)` 绑到该隧道；后续心跳若 token 变了直接拒绝。

## 12. 安全隧道（`--web-secure`）

- 首包载荷以 ASCII 魔数 `ET_WEB_NOISE_V1:` 开头 → 进入 Noise 握手；否则按明文处理。
- Noise 参数：`Noise_NN_25519_ChaChaPoly_SHA256`，prologue `easytier-webclient-noise-v1`，握手超时 3 秒。
- 握手成功后数据面再套一层 AES-GCM（`SecureDatagramSession`，含 session generation / epoch 字段）。
- **重要**：控制台侧是"机会式"的 —— 3 秒内没收到 Noise 首包就当明文。所以明文实现可以先跑通，
  安全模式再补；Go 侧用 `flynn/noise` 之类库可对齐 NN 模式。

## 13. 互操作红线（Code Review Checklist）

1. 长度前缀是否**包含** 16 字节 PeerManager 头？→ 必须包含。
2. `forward_counter` 是否写 1？→ 必须。
3. `method_index` 是否从 .proto 生成？→ 必须，且只能追加。
4. UUID 是否按 4×u32 大端分段？→ 必须。
5. 分片合并是否以首片为模板？→ 必须。
6. `RpcRequest.request` 是否嵌在 body 里，而不是直接当 body？→ 必须嵌套。
7. 响应的 `runtime_us` 是否填了？→ Rust 端不校验，但监控依赖它。
8. 心跳是否主动由设备发起？→ 是，控制台不主动 ping。

## instance_id 的语义（与上游的差异及理由）

上游：`NodeConfig.instance_id` 是一个 UUID，由实例在创建时自己生成（`easytier-proto/proto/core_config.proto:16`；Rust 侧通过 `instance.instance_id()` 取用，例如 `easytier/src/instance/factory.rs:353`），每次启动重新生成；它被当作"这一条运行中实例"的地址。

本实现：`central.InstanceID(networkID, deviceID)` 由 `sha256("easytier-console/instance/…")` 派生并按 RFC 4122 置版本位，因此**同一设备接入同一网络，重启后 instance_id 不变**。

| 维度 | 上游 | 本实现 |
|---|---|---|
| 生成时机 | 每次实例创建 | 由 (网络, 设备) 确定性派生 |
| 重启后 | 变化 | 不变 |
| 唯一性范围 | 单个运行实例 | 单个网络成员 |
| 控制面用途 | RPC 寻址、ring 连接 | 对账时判断"这台设备是否已经跑着这个网络" |

结论：两者都满足"一条实例一个标识"这个唯一被协议依赖的性质；差异在于稳定性。控制面需要稳定标识才能判断"是否需要下发"，因此这里**有意**选择确定性派生，
如果将来要与 2.7.0 的实例列表逐条对齐，需要在上游引入稳定 id 的等价物。
