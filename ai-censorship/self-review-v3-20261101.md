# v3 多轮评审记录（2026-11-01）

评审对象：初始化流程、单管理员模型、只读用户管理、实例配额、OIDC 自助绑定、多协议监听、结构化日志。
评审方法：HiDNS 代码审核团六维度，每轮独立取证，**只登记有证据的结论**。

| 轮次 | 维度 | 结论 |
|---|---|---|
| R1 | 越权 / 红蓝攻防 | **P0 命中并已修复**（见 1.1） |
| R2 | 完整从 0 部署 | 通过，1 处文档缺口 |
| R3 | 极端碰撞 | 2 项 P1、1 项 P2 |
| R4 | 漏洞渗透 | 1 项 P1（配额绕过面）、2 项 P2 |
| R5 | 前端美观 | 通过，2 项 P2 |
| R6 | 回归门禁 | 全绿 |

---

## R1 越权 / 红蓝攻防

### 1.1 P0：注册用户的会话被绑定到管理员（已修复）

**现象**：`TestPrivilegeMatrix` 中普通用户 alice 访问 `GET /api/v1/admin/users` 返回 200，
且响应里能看到管理员。攻击链：任意用户注册 → 直接获得管理员权限。

**根因**：`bal.CreateUser` 用 `Conn.Exec` 的返回值当主键，
而 DAL 的 `Exec` 返回的是 **RowsAffected**（恒为 1）。于是每个新用户的 `user.ID` 都是 1，
会话表写入的 `user_id` 指向 id=1 的管理员。
这不是"权限判断写错"，是**主键来源错误**，静态阅读极易漏掉。

**修复**：两种方言统一使用 `INSERT ... RETURNING id`（SQLite 3.35+ 与 PostgreSQL 都支持）。
**回归**：`TestPrivilegeMatrix` 现在逐条断言普通用户对 5 个管理端点的返回值必须为 403；
`internal/db` 的双数据库用例覆盖新建用户的 id 递增。

**教训（写入规范）**：新写入路径只要需要自增主键，必须走 `RETURNING`，禁止把 `Exec` 的返回值当 id。

### 1.2 角色矩阵（当前实测）

| 调用者 | 端点 | 期望 | 实测 |
|---|---|---|---|
| 匿名 | `/api/v1/admin/*` | 401 | 401 |
| 普通用户 | `GET /admin/users` | 403 | 403 |
| 普通用户 | `POST /admin/users/{id}/disabled` | 403 | 403 |
| 普通用户 | `DELETE /admin/users/{id}` | 403 | 403 |
| 普通用户 | `GET|PUT /admin/settings` | 403 | 403 |
| 管理员 | `DELETE /admin/users/{自己的 id}` | 403 | 403 |
| 管理员 | `POST /admin/users/{自己}/disabled` | 403 | 403 |
| 管理员 | `/admin/users/{id}/password`、`/{id}/oidc` | 端点不存在 | 404 |

"只读管理"是通过**不实现这些端点**保证的，而不是靠前端隐藏。

### 1.3 未初始化绕过

未初始化时除 `/healthz` 与 `/api/v1/setup` 外，所有 `/api/*` 返回
409 `setup_required`；静态资源仍可取（否则初始化页打不开）。实测：
`/api/v1/machines` → 409，`/setup` → 200，第二次 `POST /setup` → 409 `already_initialized`。

---

## R2 完整从 0 部署

空数据库 → 起进程 → 打开页面 → 初始化 → 登录 → 管理，全程仅依赖文档：

```
setup-status: {"initialized":false}      gate: 409      SPA /setup: 200
POST /setup: {"role":"admin"}            again: 409
login: ok                                admin users: 1 行
register alice: {"role":"user"}          ws route: 400（未带 Upgrade 头）
```

**发现（P2）**：README 未写"未初始化时只有 `/api/v1/setup` 可用"，运维会误判为鉴权故障。
已在本文件登记，后续并入 README 的故障排查段。

---

## R3 极端碰撞

1. **P1 并发初始化**：两个 `POST /api/v1/setup` 同时到达时，`CreateAdmin` 先 `COUNT` 后 `INSERT`，
   中间存在竞态窗口，第二个请求会撞 `username` 唯一索引而失败（返回 409，不会产生两个管理员）。
   **结论：管理员唯一性守住了，但错误信息是"用户名已存在"而非"已初始化"。**
   缓解：`users.username` 唯一索引 + 初始化标记；建议改为事务内 `INSERT ... ON CONFLICT DO NOTHING` + 影响行数判断。
2. **P1 配额与在线实例的统计口径**：管理端列表的 `online_instances` 来自心跳上报的
   `running_network_instances`，删除拦截依赖它。若设备在被删除瞬间掉线，统计会归零从而放行删除。
   缓解：删除前二次校验（当前实现已在同一请求内读取，窗口很小，但仍非原子）。
   建议：删除用户时在事务里复查 `devices` 中在线标记或加短暂冷却。
3. **P2 时区/时钟**：所有时间列存 UTC；前端按本地时区渲染。若 IdP 比控制台快 >90s，OIDC 会因
   `iat` 在未来而拒绝——这是有意的，但错误文案只显示 "issued in the future"，运维不易定位。

---

## R4 漏洞渗透

**攻击面枚举与实测**

| 攻击 | 结果 |
|---|---|
| 伪造 OIDC state（改 state 参数） | 400（HMAC 校验失败） |
| 重放旧 state cookie | 400（`expires` 10 分钟） |
| 用 http 回调地址 | 400（`callback url must use https`，除非 loopback + 显式开启） |
| 用别的 host 的回调地址 | 400（`must use the current host`） |
| 上传路径伪造用户名 `/@up/admin` 但 token 是 alice | 会话在首次心跳被拒（`upload path user does not match token`） |
| 管理员接口 IDOR（改别人的 userID） | 允许——**这是设计**：管理员本来就管所有用户；普通用户被 403 拦住 |
| 未登录访问 account 绑定 | 401 |
| 绑定回调时换账号（state 里的 uid 与当前会话不符） | 401 `bind session does not match` |

**P1：配额只在下发侧强制**。`central.Reconciler` 与 `POST /networks/{id}/members` 会检查配额，
但设备侧自发运行的第 N+1 个实例（不经过控制台）不会被拒绝——它只会在心跳里上报，控制台计数超限但无法下线。
建议：控制台在检测到超限时主动 `RetainNetworkInstance` 收敛，并把超限写进审计日志。

**P2**：`proxy-rpc` 仍是通用转发（13 个服务的任意方法）。当前只在"自己的设备"范围内，
但一旦出现分享设备的场景，需要按方法白名单收紧。

**P2**：`oidc_client_secret` 在管理端只回显 `set/not set`，但 `PUT` 时是全量覆盖语义，
并发编辑会互相覆盖；建议改为逐字段 PATCH 语义（当前已是按字段 `Update`，风险较低）。

---

## R5 前端美观

- token 一致性：新增的 setup/admin/account 页面全部使用 `--u-*` 阶梯与语义色，未出现硬编码 px；
  黄金比例分栏（`.phi-split` 61.8/38.2）在初始化页复用，与登录页一致。
- 三态完整：列表页有 loading（Skeleton）/ empty / error；删除按钮在 `online_instances > 0` 时禁用并给 tooltip。
- **P2**：管理端表格在 <640px 时只做横向滚动，未做卡片降级——与设备页存在相同问题，需统一处理。
- **P2**：`settings-page` 的 Client Secret 用"留空表示不修改"的即时保存，缺少显式的"清除"操作。

---

## R6 回归门禁

```
go build ./... && go vet ./...                                  ✅
EASYTIER_TEST_PG_DSN=... go test ./...                          ✅ arch/central/db/e2e/httpapi/oidc/rpc
(cd client && npm run build)                                    ✅ 269 kB / gzip 83 kB
```

新增用例：`TestSetupGate`、`TestPrivilegeMatrix`、`TestAdminCannotMutateTheAdministrator`、
`TestReservedUsernamesAndPolicy`、`TestDeleteBlockedByOnlineInstances`（含真实设备会话）。

---

## 结论

P0 已清零（1 项，已修复并有回归）。P1 三项、P2 六项登记在案。
本轮交付可进入内测，**不建议直接上生产**：UDP 传输缺失与 `NetworkConfig` 字段映射两项旧 P0 仍未解决。
