# easytier-console（Go 重写）

EasyTier Web 控制面重写：**协议层逐字节对齐 2.7.0**，控制面重新实现，
持久化采用**四层数据架构 + 声明式 Schema 对齐**，前端为 React 设计系统，
支持 **SQLite / PostgreSQL** 与 **完整 OIDC**。

- 项目规范总纲：`root.md`
- 协议规范：`docs/PROTOCOL.md`
- 重写方案与风险：`docs/REWRITE-PLAN.md`
- 前端设计系统（黄金比例 / 全局背景）：`docs/DESIGN.md`

## 首次部署（初始化）

空数据库启动后控制台处于**未初始化**状态：除 `/healthz` 与 `/api/v1/setup` 外，
所有 `/api/*` 返回 **409 `setup_required`**；静态资源仍可访问，以便打开初始化页。

1. 打开 `http://<host>:11211/` ，前端会跳到 `/setup`。也可直接调用接口：

   ```bash
   curl -s localhost:11211/api/v1/setup
   # {"initialized":false}
   curl -s -X POST localhost:11211/api/v1/setup -H 'Content-Type: application/json'      -d '{"username":"root-owner","password":"Sup3r-Secret-Pass!"}'
   # {"role":"admin","username":"root-owner"}
   ```

2. 初始化会创建**唯一的超级管理员**并自动登录；此后 `/api/v1/setup` 永久返回 **409 `already_initialized`**。
3. 管理员登录后在「系统设置」配置 OIDC、配额、密码策略、保留用户名、额外监听；在「用户管理」禁用/删除用户（只读，不能改密码/凭证/绑定）。

一键自检：`bash scripts/smoke-deploy.sh`（构建 → 空库 → 初始化 → 登录 → 注册 → 头检查）。

**初始化窗口是一个提权面**：还没初始化时 `/api/v1/setup` 对任何人开放，谁先访问谁就成为管理员。在公网地址上部署时，启动前设置 `EASYTIER_SETUP_TOKEN`（或 `--setup-token`），初始化请求必须带上 `X-Setup-Token` 头；没设置时进程会打印一行 WARNING。

**常见误判**：如果你的请求全部 409 `setup_required`，不是鉴权坏了，是还没初始化。

## 反向代理

TLS 由反向代理终止时（nginx / Caddy / 各家负载均衡），到进程的连接是明文：

- 在「系统设置」里打开 `server.trust_proxy`，代理转发 `X-Forwarded-For` 与 `X-Forwarded-Proto`；
- 打开后 OIDC 回调会按 `X-Forwarded-Proto: https` 推导成 https；**关闭时任何转发头都不被采信**（防止伪造）。
- 代理需要保留 `Host` 头（nginx 默认 `proxy_set_header Host $host` 即可），否则回调地址的 host 校验会失败。

## 环境变量

> **OIDC 的优先级**：`EASYTIER_OIDC_*` 只用于**首次启动的种子**；一旦管理员在后台保存了 OIDC 设置，
> 以数据库为准（后台关闭则以后台为准，此时环境变量里的 provider 作为兜底仍在用）。
> 启动日志会说明当前走的是哪一份配置。

进程只从环境读取配置（数据库与 OIDC 都支持"先读环境、后在界面覆盖"）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `EASYTIER_DB_DRIVER` | 必填 | `sqlite` / `postgres`（也接受 `sqlite3` / `pg`） |
| `EASYTIER_DB_DSN` | 必填 | 文件路径或连接串 |
| `EASYTIER_DB_MAX_OPEN` | 1（sqlite）/ 16（pg） | 连接池上限 |
| `EASYTIER_DB_MAX_IDLE` | 同上 | 空闲连接数 |
| `EASYTIER_DB_CONN_LIFETIME` | 1800（秒） | 连接最大存活时间 |
| `EASYTIER_DB_AUTO_ALIGN` | 1 | 启动时对齐 schema |
| `EASYTIER_DB_ALIGN_DRYRUN` | 0 | 只报告将要执行的变更，不落库 |
| `EASYTIER_DB_BACKUP` | 1 | 变更前写快照 |
| `EASYTIER_DB_BACKUP_DIR` | . | 快照目录 |
| `EASYTIER_DB_BACKUP_CMD` | 空 | PostgreSQL 的 dump 命令（不设则不快照，对齐仍继续） |
| `EASYTIER_SETUP_TOKEN` | 空 | 初始化令牌；设置后 `POST /api/v1/setup` 必须带 `X-Setup-Token` |
| `EASYTIER_LOG_LEVEL` | info | `trace/debug/info/warn/error` |
| OIDC | 空 | `EASYTIER_OIDC_ISSUER` / `EASYTIER_OIDC_CLIENT_ID` / `EASYTIER_OIDC_CLIENT_SECRET` / `EASYTIER_OIDC_SCOPES` / `EASYTIER_OIDC_REDIRECT_URL` / `EASYTIER_OIDC_BUTTON_LABEL` / `EASYTIER_OIDC_ALLOW_INSECURE`：仅用于首次引导，之后以界面设置为准 |

**快照失败会中止启动**：需要改 schema 时，备份写不出来就不执行任何 DDL（宁可起不来也不半途改数据）；schema 已是最新时不会备份，所以这条只影响"确实要升级"的那次启动。

测试专用（生产不要设置）：`EASYTIER_TEST_PG_DSN`（启用 PostgreSQL 用例）、`EASYTIER_SOAK_ROUNDS`（浸泡轮数）、`EASYTIER_RACE`（门禁加 `-race`）。

## 用 curl 走一遍（初始化之后）

最小闭环：登录 → 注册普通用户 → 建网络 → 设备接入 → 加成员 → 覆盖配置。
请求体与 `scripts/smoke-deploy.sh` 用的是同一套形状，那条脚本每次门禁都会跑，所以它们不会悄悄失效。

`bash`
# 登录（保存 cookie）
curl -s -c admin.jar -X POST localhost:11211/api/v1/auth/login -H 'Content-Type: application/json' -d '{"username":"root-owner","password":"Sup3r-Secret-Pass!"}'

# 注册普通用户（管理员不能代设密码，用户自己来）
curl -s -c alice.jar -X POST localhost:11211/api/v1/auth/register -H 'Content-Type: application/json' -d '{"username":"alice","password":"An0ther-Strong-Pass!"}'

# 建网络：网络名由 display_name 派生，设备侧看到的就是它
curl -s -b alice.jar -X POST localhost:11211/api/v1/networks -H 'Content-Type: application/json' -d '{"settings":{"display_name":"manual","networking_method":"Standalone","virtual_cidr":"10.88.0.0/29"}}'

# 设备接入（另开终端，用户名即 token）
# easytier-core --config-server tcp://127.0.0.1:22020/alice

# 把设备加进网络（device_id 来自 GET /api/v1/machines）
curl -s -b alice.jar -X POST localhost:11211/api/v1/networks/NETWORK_ID/members -H 'Content-Type: application/json' -d '{"device_ids":["MACHINE_ID"]}'

# 成员级覆盖：JSON 以字符串传入，设备下一次心跳即收到新配置
curl -s -b alice.jar -X PUT localhost:11211/api/v1/networks/NETWORK_ID/members/MACHINE_ID/config -H 'Content-Type: application/json' -d '{"override_config":"{"mtu":1300}"}'
``

两个实际操作里会撞到的点：

- **网络名由 `display_name` 派生**：请求里没有独立的 `network_name` 字段；
- **修订未变不会重新下发**：设备已经拿到最新配置时，再接入不会有新的下发，这是有意为之，不是连接失败。

## 质量与评审

- 目标与验收：`docs/GOALS.md`
- 审核团章程（六角色、每轮动作、门禁）：`docs/REVIEW-TEAM.md`
- 问题台账（持续跟进）：`ai-censorship/ledger.md`
- **项目规范总纲：`root.md`（分层 / 并发 / 密钥 / 前端 / 测试 / 文档 / 门禁，每条标注机器检查位置）**
- 评审记录：`ai-censorship/review-*-r*.md`

## 架构

```
cmd/easytier-webd        进程装配（数据库只从环境变量读取）
internal/wire            TCP / WS 传输帧 + PeerManager 头
internal/rpc             双向 RPC（分片、事务、压缩、错误）
internal/session         会话、心跳、身份绑定、向下调用
internal/central         网络 intent 编译 + 5s 对账
internal/httpapi         REST + 通用 proxy-rpc + OIDC 回调
internal/oidc            discovery / PKCE / JWKS / JWT 校验
internal/db              dl 驱动层 / dal 抽象层 / dsm 声明式 Schema / bal 业务适配器
internal/arch            架构守卫测试（越界即失败）
client/                  React 18 + Vite + Tailwind 客户端
```

数据访问只有一条合法调用链：`httpapi/session/central → bal → dal → dl`。
`internal/arch` 会在任何业务包 import `database/sql` 或 `dal/dl/dsm` 时让构建失败。

## 快速开始


账号由首次访问的 /setup 创建（v3 起没有种子用户参数；初始化前除 /api/v1/setup 外一律 409）。

```bash
export EASYTIER_DB_DRIVER=sqlite
export EASYTIER_DB_DSN=./et.db

go build ./... && go test ./...

(cd client && npm install && npm run build)

go run ./cmd/easytier-webd --secure-cookies=false --web-dir client/dist

easytier-core --config-server tcp://127.0.0.1:22020/admin
```

PostgreSQL：

```bash
export EASYTIER_DB_DRIVER=postgres
export EASYTIER_DB_DSN='postgres://user:pass@127.0.0.1:5432/easytier?sslmode=disable'
go run ./cmd/easytier-webd --secure-cookies=false
```

OIDC：见 `.env.example`。设置 `EASYTIER_OIDC_ISSUER` / `CLIENT_ID` /
`CLIENT_SECRET` / `REDIRECT_URL` 后登录页自动出现 SSO 按钮；
回调地址必须是 `<console>/api/v1/auth/oidc/callback`。

## 参数

| flag | 默认 | 说明 |
|---|---|---|
| `--config-server` | `0.0.0.0:22020` | 控制面 TCP 监听（长度前缀帧） |
| `--config-server-ws` | 空 | 可选 WebSocket 控制面监听 |
| `--http` | `0.0.0.0:11211` | REST + 静态前端 |
| `--web-dir` | 空 | 指向 `client/dist`，由 Go 直接托管 SPA |
| `--secure-cookies` | `true` | 明文 HTTP 调试时置 false |
| `--allow-auto-create-user` | `false` | 未知 token 自动建账号（慎用） |
| `--heartbeat-interval/--heartbeat-timeout` | `3500ms/15s` | 必须满足 timeout ≥ interval+5s |

## 门禁

```bash
go build ./... && go vet ./... && go test ./...
(cd client && npm run build)                                   # tsc -b + vite
EASYTIER_TEST_PG_DSN='postgres://...' go test ./internal/db/... # 双数据库
```

## 已知限制

Noise 安全模式、Webhook 外部鉴权、GeoIP 尚未实现（UDP 传输已实现）；
`NetworkConfig` 全字段 JSON↔proto 映射仍待生成。详见 `docs/REWRITE-PLAN.md` §9。
