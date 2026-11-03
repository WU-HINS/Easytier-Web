# easytier-console 重写方案（Go · SQLite + PostgreSQL）

> 基线：`EasyTier/Easytier@24e0303`（easytier-web 2.7.0）
> 本文回答三件事：**改什么语言、协议层怎么保真、2.7.0 的能力怎么留住**。

---

## 1. 结论先行

1. **语言选 Go**，理由不是"Go 更好"，而是这个仓库已经把 Go 侧的线协议资产铺好了：
   `easytier-go/proto/{web,api/manage,api/config,api/instance,common,error,acl,peer_rpc}`
   是同一批 `.proto` 生成的 Go 绑定，重写方不需要碰 protoc 版本/命名风格问题。
2. **协议层照抄，控制面重写**。传输帧、PeerManager 头、RpcPacket、方法索引、UUID 编码五项是**不可变**的
   （见 `docs/PROTOCOL.md`）；REST 路由、DB、业务编排可以完全另写。
3. **数据库双栈的正确姿势是"一份 SQL 模板 + 方言适配层"**，而不是两套 DAO：
   `?` 占位符 + `rebind`、时间/布尔两个转换函数，DDL 保持两份但结构性一一对应。
4. **2.7.0 的"网络"是声明式 intent + 纯函数编译 + 周期对账**，重写必须保留这三个性质，
   否则会退化成"点一下改一台机器"的命令式控制台。
5. 本目录已经是一个**可编译、可测试、双数据库跑通**的骨架（5.5k 行 Go + 270 行 SQL），
   覆盖了协议层与"网络/设备"两条主链路；UDP 可靠层、Noise、OIDC、Webhook、前端**尚未实现**，见 §9。

---

## 2. 为什么是 Go（以及为什么不是别的）

| 候选 | 优势 | 致命短板 | 结论 |
|---|---|---|---|
| **Go** | 现成 protobuf 绑定；单静态二进制（契合 musl 分发）；pgx + modernc/sqlite 全纯 Go（无 cgo）；协程模型匹配"每设备一条长连接 + 双向 RPC" | 缺泛型友好的 RPC 代码生成；UDP 可靠层要手写 | **选它** |
| Rust（原地重写） | 零协议风险 | 等于没换语言 | 否 |
| TypeScript/Node | 前后端同语言 | 无现成 protobuf 绑定；SQLite 需 native 编译；长连接 + 定时器内存开销大 | 备选 |
| Java/Kotlin | 生态全 | 二进制体积/启动时间与"嵌入式配置服务器"定位冲突 | 否 |

**语言变更真正的成本在协议层以下**，不在语言本身：

| 层 | 行数量级 | 换语言成本 |
|---|---|---|
| REST / DB / 编译 / 对账 | 本仓库 5.5k 行 Go 已覆盖大半 | 低（照着重写） |
| TCP/WS 帧 + PeerManager + RPC | ~700 行 Rust 对应 | 中（已在本仓库完成并测试） |
| UDP 可靠传输 | ~2 500 行 Rust | **高**（SYN/SACK/重传/keepalive） |
| Noise 安全隧道 | ~400 行 Rust | 中高（NN 模式 + AES-GCM 会话） |
| Captcha / GeoIP / OIDC | ~1 500 行 Rust + 资源 | 低（换库即可） |
| 前端（Vue3 + PrimeVue，11.5k 行） | — | 不建议重写，建议原样复用 |

---

## 3. 范围切分：哪些必须"逐字节复刻"，哪些可以自由发挥

**不变量（照抄）**
1. TCP 帧 `u32 LE (16 + N) + peer hdr + payload`
2. WS 一消息一包（无前缀）
3. PeerManagerHeader 16 字节 + `forward_counter=1`
4. `method_index` = 1 基声明顺序（只能追加）
5. `RpcRequest{request}` / `RpcResponse{response|error,runtime_us}` 嵌套
6. 分片首片携带 descriptor / compression_info；合并以首片为模板
7. UUID = 4×uint32 大端分段
8. 心跳策略区间与绑定语义

**可自由实现**
- HTTP 路由形态（只要语义等价）、错误码文案
- DB 表结构与索引（本仓库已重排，见 §5）
- 中央网络的编译细节（IP 分配顺序可不同，只要确定性）
- 会话存储（Cookie 名、TTL）、限流、可观测性

---

## 4. 目标架构

```
cmd/easytier-webd        进程装配：flag → store → session manager → listeners → http
  ├── internal/wire      传输帧：TCP 长度前缀 / WS 二进制 / (Phase2: UDP)
  ├── internal/rpc       双向 RPC：分片、事务号、服务注册表、压缩、错误映射
  ├── internal/session   会话：心跳校验、策略协商、身份绑定、向下调用
  ├── internal/store     持久化：DB 抽象 + 双方言 DDL + CAS 版本
  ├── internal/central   网络 intent：编译（IP/ACL/secure）+ 5s 对账推送
  ├── internal/httpapi   REST：登录、设备、网络、proxy-rpc（dynamicpb 通用代理）
  ├── internal/auth      argon2 PHC（兼容 2.7.0 的 argon2i 哈希）
  ├── internal/pb        （生成）protobuf 绑定，来自 easytier-proto
  └── internal/pb/methodindex （生成）service → method → {index, input, output}
```

依赖方向严格单向：`httpapi → session → rpc → wire`，`central → store + rpc key 常量`。没有循环。

### 4.1 通用 proxy-rpc 的实现方式（替代 Rust 的 `json_call_method`）

Rust 侧为每个服务写了一个 typed client；Go 侧不重复这件苦力，而是：

1. 从 `methodindex` 拿到 `{index, Input, Output}` 全名；
2. `protoregistry.GlobalTypes.FindMessageByName` 取描述符 → `dynamicpb` 构造消息；
3. `protojson` 解码浏览器 JSON → 二进制；调用后 `protojson` 编码回 JSON。

好处：新增服务方法只要重新生成索引表，不写一行胶水代码；坏处：JSON 字段名遵循 protojson（camelCase），
与前端现有 `snake_case` 期望需要在 HTTP 层做一次字段名兼容（见 §9 未完成项）。

---

## 5. 数据库：一套代码，两种方言

### 5.1 方言差异与对策

| 差异点 | SQLite | PostgreSQL | 本仓库做法 |
|---|---|---|---|
| 占位符 | `?` | `$1..$n` | 统一写 `?`，`rebind()` 转换 |
| 时间 | `TEXT` RFC3339 | `TIMESTAMPTZ` | `timeArg()/parseTime()` 双向适配 |
| 布尔 | `INTEGER 0/1` | `BOOLEAN` | `boolArg()/parseBool()` |
| 自增主键 | `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGSERIAL` | 插入分支取 `LastInsertId` / `RETURNING` |
| 部分唯一索引 | 支持 | 支持 | 两边都用 `WHERE ...` |
| 多语句 DDL | 支持 | 扩展协议不支持 | 迁移器按语句切分后逐条执行 |
| 连接池 | 写者串行，建议 `MaxOpenConns=1` | 并发写 | 打开参数分方言设置 |
| 外键 | 需 `PRAGMA foreign_keys=ON` | 默认开启 | DSN 里 `_pragma=foreign_keys(1)` |

### 5.2 迁移策略

- DDL 放在 `internal/store/schema/{sqlite,postgres}/0001_init.sql`，`go:embed` 打包。
- 迁移器按文件名排序执行，记入 `schema_migrations`（本身也是方言差异点，代码里显式处理）。
- 双写期（Rust 与 Go 同时能连一个库）**不建议**：水位差会让 `managed_config_revisions` 双向抖动。见 §8 的迁移方案。

### 5.3 语义要点

- `devices`：主键 `(user_id, machine_id)`；心跳 `UPSERT` 只更新 hostname/version/os/last_seen，
  **不覆盖 alias 与 first_seen**（alias 是控制台资产，first_seen 是审计事实）。
- `managed_config_revisions`：乐观并发。无行时无条件写，有行时 CAS；冲突返回 `ErrRevisionConflict`。
- `networks` / `network_members` / `network_credentials`：完整对齐 2.7.0 语义
  （含 `Gateway%% 模式的全局唯一索引、临时成员与 dedicated credential 的互斥约束）。

---

## 6. 2.7.0 能力保留

### 6.1 "网络"（central network）

保留三件事：

1. **意图持久化**：`networks`（display_name / network_name / secret / method / peer_urls / virtual_cidr / secure_mode / acl_policy）。
2. **纯函数编译** `central.Compile(intent, members, devices) → Plan{MemberPlan{...}}`：
   - 虚拟 IP：按成员创建时间确定性分配，跳过网络/广播地址与已占用地址，尊重显式 `virtual_ipv4`；
   - instance_id：`sha256("easytier-console/instance/"+networkID+"/"+deviceID)` 生成 v4 形态 UUID → 重启不改实例身份；
   - ACL：策略 → `acl.Chain{chain_type=Inbound}`，规则按 priority 倒序展开为 per-protocol 规则；
   - secure_mode：从 `(network_secret, device_id)` 派生稳定 X25519 身份。
3. **周期对账** `central.Reconciler`：5 秒一轮，编译 → 对比 `managed_config_revisions`（内容哈希 `central:<hash>`）
   → 未变化跳过 → 变化则 `WebClientService.RunNetworkInstance` 下发 → CAS 记录。离线设备算失败但不阻塞其他成员。

### 6.2 设备列表持久

心跳即写库：`devices` 表记录 `hostname / version / device_os / client_url / first_seen / last_seen`。
"在线"是**运行时态**：由 `session.Manager.Snapshots()` 用 `last_seen + timeout` 计算，
REST 返回时把两者合并。这样重启后设备列表不丢，在线状态不脏。

---

## 7. 分阶段计划

| 阶段 | 内容 | 验收（可执行） | 估时 |
|---|---|---|---|
| **P0 协议对齐** | wire + rpc + 心跳收发；TCP 与 WS 两种传输 | 用 **真实的 Rust `easytier-core --config-server`** 连上 Go 服务端，设备出现在库中，心跳策略被接受 | 1.5 周 |
| **P1 控制面** | session/manager、store 双栈、登录、设备列表、proxy-rpc | 本仓库 `internal/e2e` 两个用例 + 手工 REST 冒烟 | 1.5 周 |
| **P2 网络** | intent 编译 + 对账 + ACL + 凭据 | 三节点真实 mesh：改 ACL → 30 秒内生效；改虚拟网段 → 成员 IP 重排 | 2 周 |
| **P3 生产化** | UDP 可靠层、Noise、OIDC、Webhook、GeoIP、限流、指标 | 各模块单测 + 与 Rust 端差分 | 3~4 周 |

P0 的验收必须先做，因为**协议层的返工成本最高**：先证明能连上，再堆业务。

---

## 8. 风险登记册

| # | 风险 | 影响 | 缓解 |
|---|---|---|---|
| R1 | 协议静默漂移（上游改字段/索引） | 全量互操作失败 | 生成 `methodindex` 入库；CI 跑 `gen_methodindex.py --check`；%.proto 变动必须显式 review |
| R2 | UDP 可靠层手写（2.5k 行） | 默认部署方式不可用 | P0/P1 强制 `--config-server tcp://`；UDP 单独排期，先用抓包回放做差分 |
| R3 | Noise NN 零知识对齐 | 安全模式不可用 | 明文先行；Noise 用固定向量测试（msg1/msg2 互解） |
| R4 | 数据库迁移双跑 | revision 抖动、配置互相覆盖 | 单写者切换：停 Rust → 迁移 → 起 Go；不接受双写 |
| R5 | secure_mode 密钥派生不同 | 升级后一次全量重协商 | 文档明示；或迁移时按 Rust 算法生成并落库 |
| R6 | 前端字段名（snake_case vs protojson camelCase） | 老前端白屏 | HTTP 层做兼容映射，或前端切到 `frontend-lib` 新版本 |
| R7 | `NetworkConfig` 75 个字段的 JSON↔proto 映射 | 部分高级配置丢失 | **由 Rust 侧生成映射表**（一次性小工具 + golden 用例），不要手抄 |

R7 是本方案里唯一"量大且容易被忽略"的坑：2.7.0 的 `user_running_network_configs.network_config` 存的是
serde JSON，而设备要的是 protobuf；字段约 75 个。正确做法是写一个 Rust 小程序遍历
`easytier::common::config::NetworkConfig` 生成 Go 映射 + 双向 golden 测试，而不是人工对齐。

---

## 9. 本仓库当前状态（诚实清单）

**已实现并测试**

| 模块 | 覆盖 | 验证 |
|---|---|---|
| `internal/wire` | PeerManager 头、TCP 帧、WS 帧、监听/拨号 | 经 RPC 往返测试间接覆盖 + MTU 边界测试 |
| `internal/rpc` | 双向 mux、分片/合并、事务配对、压缩、错误 oneof | 5 个测试：往返、4KB 分片、未注册方法、32 并发、MTU |
| `internal/store` | 9 张表、迁移、CAS、双方言 | SQLite + **PostgreSQL 15 实机** 6 组测试 |
| `internal/session` | 心跳、策略、身份绑定、快照、扫除、向下调用 | e2e |
| `internal/central` | 编译（IP/ACL/secure）、对账推送、修订哈希 | 8 个编译测试 + e2e 推送/幂等测试 |
| `internal/httpapi` | 登录、console-info、summary、machines、alias、delete、networks CRUD、members、proxy-rpc | e2e |
| `internal/auth` | argon2 PHC 校验（兼容 `$argon2i$`） + MD5 预哈希兼容 | 经 e2e 登录覆盖 |
| `tools/gen_methodindex.py` | 21 服务 / 56 方法索引生成 | 生成即编译通过 |

**未实现（明确不在本次交付范围）**

- UDP 传输层（SYN/SACK/Data/Fin、重传、keepalive）—— 见 R2
- Noise 安全隧道（`--web-secure`）—— 见 R3
- Webhook 外部鉴权模式、OIDC/SSO、图形验证码、GeoIP 归属地
- 前端改造（原 Vue 前端可直接对接本 REST，字段名兼容需 §8 R6）
- `NetworkConfig` 全字段 JSON↔proto 映射（当前覆盖编译器真正决定的那部分字段）—— 见 R7
- 凭据/临时成员在运行时侧的准入（表已备好，签发/回收逻辑未接）

---

## 10. 验收命令

```bash
# 构建
go build ./...
# 全量测试（SQLite）
go test ./...
# 加上 PostgreSQL（本地实例）
EASYTIER_TEST_PG_DSN='postgres://postgres@127.0.0.1:55432/easytier_test?sslmode=disable' go test ./...
# 方法索引与 .proto 是否同步
python3 tools/gen_methodindex.py ../easytier-src/easytier-proto/proto internal/pb/methodindex/methodindex.go
git diff --exit-code internal/pb/methodindex/methodindex.go
# 启动（SQLite）
./easytier-webd --db sqlite://et.db --seed-user admin --seed-password admin --secure-cookies=false
# 启动（PostgreSQL）
./easytier-webd --db 'postgres://postgres@127.0.0.1:5432/easytier?sslmode=disable' --seed-user admin --seed-password admin
```

P0 真实互操作验收（需要 Rust 侧二进制，本次环境无法构建）：

```bash
cargo build -p easytier --bins            # 在 easytier-src 中
./easytier-webd --db sqlite://et.db --seed-user u --seed-password p --secure-cookies=false &
./target/debug/easytier-core --config-server tcp://127.0.0.1:22020/u --machine-id <uuid>
curl -b cookies http://127.0.0.1:11211/api/v1/machines   # 期望看到该 uuid online=true
```

---

# 附：v2 变更（2026-11-01）

本轮在原方案上做了三处结构性调整。

## A. 数据层改为四层（对齐 HiDNS）

原实现把 DL/DAL/BAL 混在一个包里，业务代码可以直接 import SQL 相关包。现在拆成：

| 层 | 包 | 职责 | 可被谁 import |
|---|---|---|---|
| DL | `internal/db/dl` | 连接池、方言驱动（sqlite/pgx） | 只被 `internal/db` |
| DAL | `internal/db/dal` | 方言、连接接口、查询编译器 | 只被 bal / dsm / db |
| DSM | `internal/db/dsm` | 声明式 Schema、对齐、版本、备份 | 只被 db |
| BAL | `internal/db/bal` | 业务操作（唯一合法入口） | 业务层 |

`internal/arch` 用三条测试守住边界：业务包不得 import `database/sql` 或
`dal/dal.query/dl/dsm`；只有 `internal/db/` 下可以 import `database/sql`；
`internal/db` 下不允许出现 `.sql` 文件（Schema 只能写在 Go 里）。

## B. 数据库配置与对齐

- **配置只来自环境变量**（`EASYTIER_DB_*`），删除了原来的 `--db` 参数。
  理由：容器化部署里 DSN 属于环境，不属于命令行。
- **Schema 声明在 Go**（`internal/db/dsm/complete_schema.go`），DDL 由声明推导，
  双方言不再各写一份 SQL。
- 启动时 `dsm.Init` 走：遗留检测 → 差异计划 → 快照 → 应用 → 复核 → 记录指纹。
  幂等性由 `TestAlignmentIsIdempotent` 保证（第二次打开零 DDL）。
- 已知边界：不比较索引定义、不删多余列（见 `ai-censorship/self-review-20261101.md` P1）。

## C. OIDC 与前端

- `internal/oidc` 实现完整授权码 + PKCE 流程：discovery 缓存、JWKS 缓存与轮换、
  RS256/384/512 验签、iss/aud/exp/iat/nonce 校验、RP-initiated logout。
  测试用内置假 IdP 覆盖正常流程与 5 类失败（nonce/签名/受众/过期/PKCE）。
- 前端重做为 React 设计系统（详见 `docs/DESIGN.md`）：黄金比例几何、
  自定义全局背景、明暗主题、SSO 按钮按 `/api/v1/auth/oidc/config` 动态显示。
- 前后端接口保持 2.7.0 的路由形态，仅 OIDC 三条例外新增。
