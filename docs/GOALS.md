# 目标与评审机制（v3）

> 约束基线：HiDNS `root.ai.md` 的「代码审核团 / 提交相关 / 文档规范」三节。
> 本文件是 v3 的唯一验收口径；评审记录写入 `ai-censorship/`。

## 1. 功能目标

### G1 完整日志
控制台所有关键路径（HTTP、DB、协议、认证、后台操作）输出结构化日志，格式对齐 HiDNS：
`时间 级别 [主模块] [子模块] [函数] [L行号] ["标签"] 内容`，级别由 `EASYTIER_LOG_LEVEL` 控制。

### G2 首次部署初始化
数据库连上后，若系统未初始化，**所有业务路由返回 409 `setup_required`**，前端跳转初始化页。
初始化要求创建**超级管理员**账户（唯一），成功后置 `initialized=true`，此后 `/api/v1/setup` 永久关闭。

### G3 单管理员模型
- 系统有且只有一个 `admin` 角色账户，只能由初始化流程创建。
- 管理员与普通用户**同一套登录与会话机制**，权限差异只在 `role`。

### G4 管理员用户管理（只读）
管理员可以：查看用户列表、**禁用/启用**、**删除**。
管理员不可以：改密码、改凭证、改 OIDC 绑定、代登录。
删除前置条件：**该用户有实例在线时禁止删除**（返回 409 与在线实例数）。

### G5 实例配额（全局）
后台配置「每用户可管理实例数」上限；用户实例数达到上限后：
- 新的实例下发被拒绝（`quota_exceeded`）；
- 新的设备注册（心跳首次绑定）被拒绝。

### G6 后台可配置项
OIDC（issuer/client_id/secret/按钮文案/开关）、全局背景、密码安全等级、保留用户名列表、实例配额、额外监听（UDP/TCP）。

### G7 普通用户自助
普通用户可**绑定/解绑 OIDC**；绑定只影响本账户，管理员不可代操作。
OIDC 回调地址**由前端按当前 origin 生成**，并**强制 https**（非 https 时前端禁用绑定并提示）。

### G8 实例接入通道
- WebSocket 与面板**复用同一端口**，路径 /@up/<用户名>；官方客户端另有约定：2.4.0 / 2.5.0 取**第一段**路径当心跳 token，2.6.4 起取**最后一段**，因此给客户端的控制面地址写成 ws://host:port/<用户名>（两种解析都得到正确用户名）。
- 后台可额外开启 **TCP / UDP**，与 WS **同端口号**（可自定义），可同时开启。

## 2. 评审机制（对齐 HiDNS 代码审核团）

每一轮评审产出 `ai-censorship/<模型>_review-YYYYMMDD.md`，包含：范围、发现（P0/P1/P2）、复现证据、结论。

必须覆盖六个维度，缺一不可：

| 维度 | 检查方法 |
|---|---|
| 前端美观 | 设计 token 一致性、对比度、响应式、空/错/载入三态 |
| 完整从0部署 | 空环境 → 建库 → 初始化 → 登录 → 设备接入，全程只靠文档 |
| 极端碰撞 | 并发初始化、并发删除、重复绑定、超长/非法输入、时区与时钟偏移 |
| 红蓝攻防 | 攻击面枚举：会话、CSRF、越权、注入、SSRF、枚举 |
| 漏洞渗透 | 用真实请求打：未初始化绕过、IDOR、路径穿越、token 重放 |
| 越权 | 角色矩阵逐条验证：普通用户 × 管理员端点、跨租户 IDOR |

评分口径：P0（阻断发布）/ P1（发布前必须修）/ P2（可延后但需登记）。

## 3. 验收清单（v3 实测）

每一项都指名证据：要么是有名字的用例，要么是门禁里的一步。名字由 `scripts/check-docs.mjs` 校验存在性，
写错一个不存在的测试会直接让门禁变红。

### 3.1 身份与会话

- [x] 空库时仅 `/healthz` 与 `/api/v1/setup` 可用，其余 `/api/*` 返回 409 `setup_required` — `TestSetupGate`
- [x] 初始化后 `/api/v1/setup` 永久 409；设置令牌时无头 401、错令牌 401、正确 201 — `TestSetupTokenGatesTheFirstAdmin`
- [x] 普通用户访问 `/api/v1/admin/*` 一律 403；管理员也不能改他人密码/绑定/凭证 — `TestPrivilegeMatrix`、`TestAdminCannotMutateTheAdministrator`
- [x] 用户自助改密：校验当前密码、走密码策略、新旧不得相同、成功后清空该账号全部会话并为调用方重签 — `TestPasswordRotation`
- [x] 撤销立即生效：禁用即清会话、删除用户后旧 cookie 立即 401、过期会话懒判断并即时删行 — `TestDisablingRevokesOpenSessions`、`TestSessionOfDeletedUserIsRejected`、`TestExpiredSessionIsRejected`
- [x] 会话寿命在 cookie 与数据库行之间一致（同一 TTL，行不长于 cookie） — `TestSessionTTLReachesBothTheCookieAndTheRow`
- [x] 登录限流：按账号与按地址分别计数、成功后清零、窗口过期恢复、一个地址不能锁死他人账号 — `TestLoginIsThrottled`、`TestLoginThrottleIsPerAddressToo`、`TestLoginLimiterThrottlesPerAccountAndAddress`、`TestLoginLimiterDropsExpiredWindows`
- [x] 禁用账户不能登录，但错误口令仍返回 401（不泄露账户状态） — `TestDisabledAccountCannotLogin`

### 3.2 单管理员与实例配额

- [x] "有且只有一个管理员"是数据库约束（部分唯一索引），不是应用层检查 — `TestSingleAdminIndexExists`、`TestSecondAdminIsRefused`
- [x] 并发初始化只产生一个管理员，其余请求 409 — `TestConcurrentSetupCreatesExactlyOneAdmin`
- [x] 配额在同一事务内强制，并发加成员不可突破；`quota=0` 视为不限；配额按用户而非按网络计数 — `TestAddMembersBoundedEnforcesTheQuota`、`TestConcurrentMemberAddsRespectQuota`、`TestQuotaIsPerUserNotPerNetwork`、`TestQuotaEnforcedOnHeartbeatPath`
- [x] 有在线实例的用户不可删除（409 与实例数） — `TestDeleteBlockedByOnlineInstances`

### 3.3 设备接入与撤销

- [x] `/@up/<用户名>` 路由存在、用户名与 token 必须一致、超大消息拒绝、并发上限触发 503 — `TestWSUploadChannelCarriesHeartbeat`、`TestWSUploadRejectsBadPathIdentity`、`TestWSUploadRejectsOversizedMessage`、`TestWSUploadConcurrencyIsBounded`
- [x] 阻止设备：立即断开现有隧道、拒绝新连接、可在列表解除、对其他租户不可见 — `TestBlockingDisconnectsLiveDevice`、`TestBlockedDevicesCanBeListedAndLifted`、`TestBlockDuringHeartbeatStorm`
- [x] 独立 WebSocket 控制面与额外 TCP 端口都能真的接入（冒烟断言） — 冒烟步骤 "standalone ws listener" / "extra listener open"
- [x] 空闲隧道与静默设备按时关闭 — `TestIdleTunnelIsClosed`、`TestSilentDeviceIsExpired`

### 3.4 网络与下发

- [x] 覆盖配置经对账真正到达设备（`mtu=1300` 出现在下发配置里） — 冒烟步骤 "override applied on device"
- [x] 编译确定性、地址顺序分配、ACL 选择器 fail-closed、与上游金样一致 — `TestCompileIsDeterministic`、`TestCompileAllocatesSequentialAddresses`、`TestACLSelectorFailsClosed`、`TestCompiledConfigsMatchGolden`
- [x] 修订未变不重复下发；重连后补齐；两网络在同一设备上收敛 — `TestReconcileLoopPushesWithoutManualTrigger`、`TestReconcileCatchesUpAfterReconnect`、`TestTwoNetworksPerDeviceConverge`
- [x] 坏网络不影响其它租户 — `TestBrokenNetworkDoesNotBlockOthers`

### 3.5 数据层与双方言

- [x] 整套 REST 与端到端场景在 PostgreSQL 私有 schema 上各跑一遍 — 门禁 `postgres api` / `postgres e2e`
- [x] 声明的每个索引在引擎里真实存在、唯一性与部分谓词一致 — `TestDeclaredIndexesExistInTheEngine`
- [x] 旧库缺表时对齐补齐且不动数据；DryRun 只报告不落库；旧版库只报告不迁移 — `TestMissingTableIsRecreatedWithoutDataLoss`、`TestDryRunReportsWithoutApplying`、`TestLegacyDatabaseIsOnlyReported`
- [x] 快照先于 DDL，快照失败即中止（SQLite 拷文件与 PostgreSQL 调命令两条路径） — `TestBackupRunsBeforeSchemaChange`、`TestPostgresBackupCommand`

### 3.6 部署与可观测性

- [x] 日志格式（时间戳/级别/模块/子模块/调用点/行号/标签）有断言 — `TestFormatMatchesTheDocumentedShape`
- [x] 心跳策略的边界值（1s/60s、timeout=interval+5s 等）被逐条验证，坏参数在启动即失败 — `TestHeartbeatPolicyBounds`、`TestNewManagerRejectsAnInvalidPolicy`
- [x] SIGTERM 后干净退出（停止接收→等在途请求→等待会话）并立即释放端口，同库重启数据留存 — 冒烟步骤 "process exited" / "setup status after restart"

### 3.7 仍未做（诚实清单）

- UDP 传输层未实现：设置已持久化，界面标注"传输层待实现"，对账不会使用它。
- Docker / Compose 参考编排未在实机验证（当前环境没有 daemon）。
- 多副本共库可运行，但两个进程会各自上报同一设备（已有用例与告警，未做租约）。
- 有状态 fuzz 目前是固定种子的确定性序列，未做长跑；线格式模糊由 `scripts/fuzz.sh` 单独执行。

评审记录：`ai-censorship/` 下按轮次编号；账本 `ai-censorship/ledger.md` 是最新结论。

## 4. 评审历史（持续跟进）

| 轮次 | 记录 | 关键结论 |
|---|---|---|
| r1 | `ai-censorship/self-review-v3-20261101.md` | 六维度首轮；P0 提权（主键来源）当场修复 |
| r2 | `ai-censorship/review-20261101-r2.md` | r1 复验（2 项） |
| r3 | `ai-censorship/review-20261101-r3.md` | r2 复验（1 项） |
| r4 | `ai-censorship/review-20261101-r4.md` | r3 复验（1 项） |
| r5 | `ai-censorship/review-20261101-r5.md` | r4 复验（0 项） |
| r6 | `ai-censorship/review-20261101-r6.md` | R-E 渗透：F-019 驱动错误外泄（P1）（3 项） |
| r7 | `ai-censorship/review-20261101-r7.md` | 流程自审（1 项） |
| r8 | `ai-censorship/review-20261101-r8.md` | R-B 部署：F-023（P0）全新 PostgreSQL 库启动失败（3 项） |
| r9 | `ai-censorship/review-20261101-r9.md` | 换前提：生产 TLS 拓扑（1 项） |
| r10 | `ai-censorship/review-20261101-r10.md` | 换前提：多副本共库（1 项） |
| r11 | `ai-censorship/review-20261101-r11.md` | 换前提：离线与恢复（1 项） |
| r12 | `ai-censorship/review-20261101-r12.md` | 换前提：恶意协议输入（2 项） |
| r13 | `ai-censorship/review-20261101-r13.md` | WebSocket 接入面（3 项） |
| r14 | `ai-censorship/review-20261101-r14.md` | 机器驱动：协议层 fuzz（1 项） |
| r15 | `ai-censorship/review-20261101-r15.md` | 换前提：数据竞争（3 项） |
| r16 | `ai-censorship/review-20261101-r16.md` | 并发压测（1 项） |
| r17 | `ai-censorship/review-20261101-r17.md` | 装配缝：P0 又出现一次（2 项） |
| r18 | `ai-censorship/review-20261101-r18.md` | 把端到端接进冒烟（2 项） |
| r19 | `ai-censorship/review-20261101-r19.md` | 从"能收到"到"收得对"（2 项） |
| r20 | `ai-censorship/review-20261101-r20.md` | 补齐 2.7.0 的编辑面（3 项） |
| r21 | `ai-censorship/review-20261101-r21.md` | 写入面系统性复查（3 项） |
| r22 | `ai-censorship/review-20261101-r22.md` | 前后端契约（2 项） |
| r23 | `ai-censorship/review-20261101-r23.md` | 按反向清单补 UI，顺手挖到一个 P1（2 项） |
| r24 | `ai-censorship/review-20261101-r24.md` | 设备操作接口：先证明它能用（2 项） |
| r25 | `ai-censorship/review-20261101-r25.md` | 路由表界面与 payload 契约（2 项） |
| r26 | `ai-censorship/review-20261101-r26.md` | 网络设置面收口（2 项） |
| r27 | `ai-censorship/review-20261101-r27.md` | 回到后端：把字节序钉死在协议上（1 项） |
| r28 | `ai-censorship/review-20261101-r28.md` | 逐字段对齐上游编译器（2 项） |
| r29 | `ai-censorship/review-20261101-r29.md` | override 语义与下发前校验（2 项） |
| r30 | `ai-censorship/review-20261101-r30.md` | 金样测试当场抓到一个越权漏洞（3 项） |
| r31 | `ai-censorship/review-20261101-r31.md` | 故障隔离（2 项） |
| r32 | `ai-censorship/review-20261101-r32.md` | panic 边界（3 项） |
| r33 | `ai-censorship/review-20261101-r33.md` | 关停路径（1 项） |
| r34 | `ai-censorship/review-20261101-r34.md` | 未填字段的默认值，逐个核对（3 项） |
| r35 | `ai-censorship/review-20261101-r35.md` | 组网方式的必填项，按上游校准（1 项） |
| r36 | `ai-censorship/review-20261101-r36.md` | 安全模式矩阵与悬空凭据（2 项） |
| r37 | `ai-censorship/review-20261101-r37.md` | 一台设备两个网络（1 项） |
| r38 | `ai-censorship/review-20261101-r38.md` | 撤销要真的撤销（0 项） |
| r39 | `ai-censorship/review-20261101-r39.md` | 让"同步状态"可见（1 项） |
| r40 | `ai-censorship/review-20261101-r40.md` | 浸泡：设备反复上下线（1 项） |
| r41 | `ai-censorship/review-20261101-r41.md` | 交付前一致性总检（1 项） |
| r42 | `ai-censorship/review-20261101-r42.md` | 前端的"看不见的部分"（2 项） |
| r43 | `ai-censorship/review-20261101-r43.md` | 照着 README 走一遍（2 项） |
| r44 | `ai-censorship/review-20261101-r44.md` | 把"文档可执行"变成门禁（1 项） |
| r45 | `ai-censorship/review-20261101-r45.md` | 关掉挂了 36 轮的 MONITOR（0 项） |
| r46 | `ai-censorship/review-20261101-r46.md` | OIDC 绑定的接管路径（2 项） |
| r47 | `ai-censorship/review-20261101-r47.md` | 把"守卫扫描"变成方法（3 项） |
| r48 | `ai-censorship/review-20261101-r48.md` | 会话生命周期（2 项） |
| r49 | `ai-censorship/review-20261101-r49.md` | JWT 校验的负向面（2 项） |
| r50 | `ai-censorship/review-20261101-r50.md` | 关停收尾，兼半个复盘（0 项） |
| r51 | `ai-censorship/review-20261101-r51.md` | 构建产物本身（1 项） |
| r52 | `ai-censorship/review-20261101-r52.md` | 升级路径：旧库 + 有数据（1 项） |
| r53 | `ai-censorship/review-20261101-r53.md` | 两条"每次启动都会跑"的路径（2 项） |
| r54 | `ai-censorship/review-20261101-r54.md` | 旧库检测从来没接上（3 项） |
| r55 | `ai-censorship/review-20261101-r55.md` | 阻止接入是单向门（1 项） |
| r56 | `ai-censorship/review-20261101-r56.md` | 踢人要当下生效（1 项） |
| r57 | `ai-censorship/review-20261101-r57.md` | 禁用也要当下生效（1 项） |
| r58 | `ai-censorship/review-20261101-r58.md` | 守卫自己的漏洞（1 项） |
| r59 | `ai-censorship/review-20261101-r59.md` | 章程与脚本不能各说各话（1 项） |
| r60 | `ai-censorship/review-20261101-r60.md` | 前端的单一出口（1 项） |
| r61 | `ai-censorship/review-20261101-r61.md` | 极端碰撞：阻止与心跳抢同一个会话（1 项） |
| r62 | `ai-censorship/review-20261101-r62.md` | 日志格式不能有两种（1 项） |
| r63 | `ai-censorship/review-20261101-r63.md` | 日志格式没人验证过（1 项） |
| r64 | `ai-censorship/review-20261101-r64.md` | 20 个开关，文档写了 4 个（1 项） |
| r65 | `ai-censorship/review-20261101-r65.md` | 从没被连过的那个端口（1 项） |
| r66 | `ai-censorship/review-20261101-r66.md` | 配置 OIDC 之后，发生了什么（2 项） |
| r67 | `ai-censorship/review-20261101-r67.md` | 关掉挂了 53 轮的有状态 fuzz（0 项） |
| r68 | `ai-censorship/review-20261101-r68.md` | 把最后一块界面补上（0 项） |
| r69 | `ai-censorship/review-20261101-r69.md` | 覆盖配置真的会到设备吗（2 项） |
| r70 | `ai-censorship/review-20261101-r70.md` | 让"没跑"变成红色（3 项） |
| r71 | `ai-censorship/review-20261101-r71.md` | PostgreSQL 的快照有三种结局，只测过一种（1 项） |
| r72 | `ai-censorship/review-20261101-r72.md` | 七十个接口用例，从来没在 PostgreSQL 上跑过（3 项） |
| r73 | `ai-censorship/review-20261101-r73.md` | 对账和会话也只在一种库上跑过（1 项） |
| r74 | `ai-censorship/review-20261101-r74.md` | 贵的那几项检查，从没一起跑过（1 项） |
| r75 | `ai-censorship/review-20261101-r75.md` | 密码泄露了，用户自己改不了（1 项） |
| r76 | `ai-censorship/review-20261101-r76.md` | 两个并发 setup，两个管理员（3 项） |
| r77 | `ai-censorship/review-20261101-r77.md` | 声明了的索引，引擎里真的建了吗（1 项） |
| r78 | `ai-censorship/review-20261101-r78.md` | 配额是策略，不是约束（1 项） |
| r79 | `ai-censorship/review-20261101-r79.md` | 同名首次 SSO 登录（1 项） |
| r80 | `ai-censorship/review-20261101-r80.md` | 同一个错误码，两处写法（1 项） |
| r81 | `ai-censorship/review-20261101-r81.md` | 把"人工逐个发现"变成规则（1 项） |
| r82 | `ai-censorship/review-20261101-r82.md` | 限流器：一个测试都没有（2 项） |
| r83 | `ai-censorship/review-20261101-r83.md` | 会话寿命写在两个地方（1 项） |
| r84 | `ai-censorship/review-20261101-r84.md` | 心跳的边界规则（2 项） |
| r85 | `ai-censorship/review-20261101-r85.md` | 验收清单已经落后八十轮（1 项） |
| r86 | `ai-censorship/review-20261101-r86.md` | 照文档走一遍（第二遍）（1 项） |
| r87 | `ai-censorship/review-20261101-r87.md` | "和冒烟脚本一致"这句话，谁来检查（1 项） |
| r88 | `ai-censorship/review-20261101-r88.md` | 密钥不会出现在日志里吗（1 项） |
| r89 | `ai-censorship/review-20261101-r89.md` | 网络密钥会不会被顺手带出来（1 项） |
| r90 | `ai-censorship/review-20261101-r90.md` | 从"逐个端点"到"扫一遍"（1 项） |
| r91 | `ai-censorship/review-20261101-r91.md` | 前端的两条安全规则，只有代码没有证据（1 项） |
| r92 | `ai-censorship/review-20261101-r92.md` | 一条从来没看过东西的守卫（2 项） |
| r93 | `ai-censorship/review-20261101-r93.md` | 让"门禁全绿"本身可被质疑（2 项） |
| r94 | `ai-censorship/review-20261101-r94.md` | 把审计往数据层推一层（1 项） |
| r95 | `ai-censorship/review-20261101-r95.md` | 冒烟里那条永远为真的断言（2 项） |
| r96 | `ai-censorship/review-20261101-r96.md` | 记录不等于断言（1 项） |
| r97 | `ai-censorship/review-20261101-r97.md` | 规范需要一个入口（2 项） |
| r98 | `ai-censorship/review-20261101-r98.md` | 状态码也只被记录，不被断言（1 项） |
| r99 | `ai-censorship/review-20261101-r99.md` | "缺了就跳过"也是一种放过（1 项） |
| r100 | `ai-censorship/review-20261101-r100.md` | 满一百轮的状态盘点（1 项） |
| r101 | `ai-censorship/review-20261101-r101.md` | 把最后两条断言也打红（1 项） |
| r102 | `ai-censorship/review-20261101-r102.md` | 越权矩阵只认识五个端点（1 项） |
| r103 | `ai-censorship/review-20261101-r103.md` | 租户隔离靠"每个人都记得"（2 项） |
| r104 | `ai-censorship/review-20261101-r104.md` | 手写 SQL 是租户守卫的盲区（1 项） |
| r105 | `ai-censorship/review-20261101-r105.md` | 认证数据没有缓存策略（1 项） |
| r106 | `ai-censorship/review-20261101-r106.md` | 少了一条头（1 项） |
| r107 | `ai-censorship/review-20261101-r107.md` | 跨站守卫只在一个路由上验证过（1 项） |
| r108 | `ai-censorship/review-20261101-r108.md` | 读路由没有守卫（1 项） |
| r109 | `ai-censorship/review-20261101-r109.md` | 规范落后了八轮（1 项） |
| r110 | `ai-censorship/review-20261101-r110.md` | "文案都在 copy 表里"是句空话（1 项） |
| r111 | `ai-censorship/review-20261101-r111.md` | 基线开始下降（0 项） |
| r112 | `ai-censorship/review-20261101-r112.md` | 两个大页面也迁走了（0 项） |
| r113 | `ai-censorship/review-20261101-r113.md` | 基线归零（0 项） |
| r114 | `ai-censorship/review-20261101-r114.md` | 迁完之后先跑一遍全量（1 项） |
| r115 | `ai-censorship/review-20261101-r115.md` | 深检没有记录（1 项） |
| r116 | `ai-censorship/review-20261101-r116.md` | 三份没人读的文档（1 项） |
| r117 | `ai-censorship/review-20261101-r117.md` | 一次假阳性，和一条真断言（1 项） |
| r118 | `ai-censorship/review-20261101-r118.md` | 注销从来没被测过（4 项） |
| r119 | `ai-censorship/review-20261101-r119.md` | 核对为什么没早点发现（2 项） |
| r120 | `ai-censorship/review-20261101-r120.md` | 后台清理有没有人看着（2 项） |
| r121 | `ai-censorship/review-20261101-r121.md` | 一直在动的背景（1 项） |
| r122 | `ai-censorship/review-20261101-r122.md` | 红色的字，读屏听不见（1 项） |
| r123 | `ai-censorship/review-20261101-r123.md` | 键盘用户要按十几次 Tab（1 项） |
| r124 | `ai-censorship/review-20261101-r124.md` | debug 档什么都不多（1 项） |
| r125 | `ai-censorship/review-20261101-r125.md` | 两份配置，谁说了算（1 项） |
| r126 | `ai-censorship/review-20261101-r126.md` | 端口被占了，它说它起来了（1 项） |
| r127 | `ai-censorship/review-20261101-r127.md` | 同族的第二处，和一次不合格的探针（1 项） |
| r128 | `ai-censorship/review-20261101-r128.md` | 顺序用例通过，不代表并发安全（2 项） |
| r129 | `ai-censorship/review-20261101-r129.md` | 28 处把内部错误念给客户端听（1 项） |
| r130 | `ai-censorship/review-20261101-r130.md` | 被跳过的证据也是"通过"（1 项） |
| r131 | `ai-censorship/review-20261101-r131.md` | 同一个比例，四种写法（2 项） |
| r132 | `ai-censorship/review-20261101-r132.md` | 名单里写着两份文件，遍历从没走到过它们（1 项） |
| r133 | `ai-censorship/review-20261101-r133.md` | 五年只查五组颜色（3 项） |
| r134 | `ai-censorship/review-20261101-r134.md` | 六个评审同时开工（11 项） |
| r135 | `ai-censorship/review-20261101-r135.md` | 收 OPEN 单，以及一次"探针打错人"（1 项） |
| r136 | `ai-censorship/review-20261101-r136.md` | 转发头与第二条隧道（0 项） |
| r137 | `ai-censorship/review-20261101-r137.md` | 配置插值、身份冲突，以及最后一根 OPEN（0 项） |
| r138 | `ai-censorship/review-20261101-r138.md` | 最后一条 OPEN：守卫要证明自己没在撒谎（0 项） |
| r139 | `ai-censorship/review-20261101-r139.md` | 22 项全绿，包括那条按需的（0 项） |
| r140 | `ai-censorship/review-20261101-r140.md` | 第二轮团队：去证伪，而不是再找一遍（9 项） |
| r141 | `ai-censorship/review-20261101-r141.md` | 整行写回的病，治在一个函数上（0 项） |
| r142 | `ai-censorship/review-20261101-r142.md` | 把"从 0 部署"这条路上最后两条 OPEN 收掉（0 项） |
| r143 | `ai-censorship/review-20261101-r143.md` | 把最后一个"界面上到不了"补上（2 项） |
| r144 | `ai-censorship/review-20261101-r144.md` | 把一个看了几十轮的数字变成断言（0 项） |
| r145 | `ai-censorship/review-20261101-r145.md` | 把最后一个"空开关"接上（1 项） |
| r146 | `ai-censorship/review-20261101-r146.md` | 先审自己上一轮刚写的代码（1 项） |
| r147 | `ai-censorship/review-20261101-r147.md` | 给新传输加脏流量与竞态（0 项） |
| r148 | `ai-censorship/review-20261101-r148.md` | 我上一轮自己标出来的空白（1 项） |
| r149 | `ai-censorship/review-20261101-r149.md` | 顺着"id 唯一"的假设往下查（1 项） |
| r150 | `ai-censorship/review-20261101-r150.md` | 第三轮团队：专审最近十轮的新代码（9 项） |
| r151 | `ai-censorship/review-20261101-r151.md` | 收第三轮团队留下的六条（0 项） |
| r152 | `ai-censorship/review-20261101-r152.md` | 反射面与去重的最后两处（1 项） |
| r153 | `ai-censorship/review-20261101-r153.md` | 把最后一条 OPEN 关掉：扫描器看不见的东西（0 项） |
| r154 | `ai-censorship/review-20261101-r154.md` | 一次达成度自评，而不是又找一个问题（0 项） |
| r155 | `ai-censorship/review-20261101-r155.md` | 把两条 MONITOR 变成可判定的（0 项） |
| r156 | `ai-censorship/review-20261101-r156.md` | 最后一条 MONITOR：去上游找答案（0 项） |
| r157 | `ai-censorship/review-20261101-r157.md` | 让设备真的走一次 UDP（1 项） |
| r158 | `ai-censorship/review-20261101-r158.md` | 让"重复投递"这件事在真实传输上发生一次（1 项） |
| r159 | `ai-censorship/review-20261101-r159.md` | 把"没做过长跑"从借口变成数字（1 项） |
| r160 | `ai-censorship/review-20261101-r160.md` | 第四轮团队：只审最近三轮的新代码（4 项） |
| r161 | `ai-censorship/review-20261101-r161.md` | 给上一轮的修复补用例，其中一条补不上（0 项） |
| r162 | `ai-censorship/review-20261101-r162.md` | 上一轮说"测不了"，其实是我测错了对象（0 项） |
| r163 | `ai-censorship/review-20261101-r163.md` | 把守卫接进门禁（第 25 项）（0 项） |
| r164 | `ai-censorship/review-20261101-r164.md` | 先量一量"没有守卫的 FIXED"有多少（0 项） |
| r165 | `ai-censorship/review-20261101-r165.md` | 给"立即撤销"补上它一直缺的断言（0 项） |
| r166 | `ai-censorship/review-20261101-r166.md` | 编号不再手写（0 项） |
| r167 | `ai-censorship/review-20261101-r167.md` | 把守卫的名字写进账本（0 项） |
| r168 | `ai-censorship/review-20261101-r168.md` | 86/106 能对上，但我不会批量改账本（0 项） |
| r169 | `ai-censorship/review-20261101-r169.md` | 20 条逐条看，其中 4 条是真的缺（0 项） |
| r170 | `ai-censorship/review-20261101-r170.md` | 五条真缺口里，第一条其实早就有守卫（0 项） |
| r171 | `ai-censorship/review-20261101-r171.md` | 按新纪律先 grep，再下结论（0 项） |
| r172 | `ai-censorship/review-20261101-r172.md` | 三条里最后一条是可测的（0 项） |
| r173 | `ai-censorship/review-20261101-r173.md` | 又一次"我先说它坏了"（0 项） |
| r174 | `ai-censorship/review-20261101-r174.md` | 把那条说了三轮的用例写出来了（0 项） |
| r175 | `ai-censorship/review-20261101-r175.md` | 把两条不可测的写清楚，然后收线（0 项） |
| r176 | `ai-censorship/review-20261101-r176.md` | 全量门禁回归（含引用证据扫描）（0 项） |
| r177 | `ai-censorship/review-20261101-r177.md` | 第五轮团队：两个 P2，其中一个是我上一轮亲手引入的死锁（0 项） |
| r178 | `ai-censorship/review-20261101-r178.md` | 收 F-255，并第三次栽在同一个替换手法上（0 项） |
| r179 | `ai-censorship/review-20261101-r179.md` | 把说了三轮的规矩做成工具（0 项） |
| r180 | `ai-censorship/review-20261101-r180.md` | 想给审计脚本加一条用例，先确认它不是本来就红（7 项） |
| r181 | `ai-censorship/review-20261101-r181.md` | 补上那条"没有它，其他绕过永远没人发现"的用例（0 项） |
| r182 | `ai-censorship/review-20261101-r182.md` | 想在同一个文件里加第二组断言，又栽在转义上（0 项） |
| r183 | `ai-censorship/review-20261101-r183.md` | 我写了一个"永远不执行"的断言，然后把它删了（0 项） |
| r184 | `ai-censorship/review-20261101-r184.md` | 补边界值，让 off-by-one 无处可藏（0 项） |
| r185 | `ai-censorship/review-20261101-r185.md` | 给 disconnect 用例补上"零基线"（0 项） |
| r186 | `ai-censorship/review-20261101-r186.md` | 编号脚本不再静默跳过看不懂的文件名（0 项） |
| r187 | `ai-censorship/review-20261101-r187.md` | 最后一条：清空分支终于是可判定的（0 项） |
| r188 | `ai-censorship/review-20261101-r188.md` | 把"没做长时 fuzz"做掉一半（0 项） |
| r189 | `ai-censorship/review-20261101-r189.md` | 乱序：不用真 NAT 也能测（0 项） |
| r190 | `ai-censorship/review-20261101-r190.md` | 地址映射：能测的那一半也测了（0 项） |
| r191 | `ai-censorship/review-20261101-r191.md` | 把边界写回章程（0 项） |
| r192 | `ai-censorship/review-20261101-r192.md` | OIDC 只保留绑定模式（0 项） |
| r193 | `ai-censorship/review-20261101-r193.md` | 三通道真的同时接上，只推了一条（3 项） |
| r194 | `ai-censorship/review-20261101-r194.md` | 网络高级设置与"节点优先"的合并规则（2 项） |
| r195 | `ai-censorship/review-20261101-r195.md` | 实例接收限速落地（先确认协议里有这个字段）（1 项） |
| r196 | `ai-censorship/review-20261101-r196.md` | 网络白名单：命中即并网，且只并一次（1 项） |
| r197 | `ai-censorship/review-20261101-r197.md` | 给网络高级设置开一扇门（API）（1 项） |
| r198 | `ai-censorship/review-20261101-r198.md` | 给高级设置一块能点的界面（1 项） |
| r199 | `ai-censorship/review-20261101-r199.md` | 把"没验证过"变成一条可执行的检查（1 项） |
| r200 | `ai-censorship/review-20261101-r200.md` | 下载真机二进制，跑出来的第一个结论是"连不上"（1 项） |
| r201 | `ai-censorship/review-20261101-r201.md` | 真机二进制跑通了一半：设备能上线，配置还推不过去（0 项） |
| r202 | `ai-censorship/review-20261101-r202.md` | 下发被拒：排除了六种键组合和一个 peer 假设（1 项） |
| r203 | `ai-censorship/review-20261101-r203.md` | 2.4.0 到 2.7.0 全部接通，靠的是键名和一个被证伪的"改进"（1 项） |
| r204 | `ai-censorship/review-20261101-r204.md` | 把"能跑一次"打磨成"每次都能跑"（1 项） |
| r205 | `ai-censorship/review-20261101-r205.md` | ws 通了，udp 不通（附抓包）（2 项） |
| r206 | `ai-censorship/review-20261101-r206.md` | udp 修好了：16 字节是会话头，缺的是一个 Sack（1 项） |
| r207 | `ai-censorship/review-20261101-r207.md` | udp 大部分通了，但 2.6.4 退回去了（1 项） |
| r208 | `ai-censorship/review-20261101-r208.md` | 一个 padding 字节，卡住整个 2.6.4（1 项） |
| r209 | `ai-censorship/review-20261101-r209.md` | ws 的两格：不是路径，是响应（0 项） |
| r210 | `ai-censorship/review-20261101-r210.md` | ws 的两格：是 URL 约定，不是协议（0 项） |
| r211 | `ai-censorship/review-20261101-r211.md` | 换成无 tun 模式：实例终于跑起来了（2 项） |
| r212 | `ai-censorship/review-20261101-r212.md` | 用公开节点做对照实验，问题收窄到"推送的 peer_urls"（0 项） |
| r213 | `ai-censorship/review-20261101-r213.md` | 两台受管实例第一次真正连上了（1 项） |
| r214 | `ai-censorship/review-20261101-r214.md` | 数据面卡在 /dev/net/tun，不是卡在代码（0 项） |
| r215 | `ai-censorship/review-20261101-r215.md` | 给"对等"上锁，并把"做不到"写进门店（0 项） |
| r216 | `ai-censorship/review-20261101-r216.md` | 先把"到底缺什么"变成一张能复核的表（1 项） |
| r217 | `ai-censorship/review-20261101-r217.md` | 簇 C：配置 ⇄ TOML，单独一层（1 项） |
| r218 | `ai-censorship/review-20261101-r218.md` | 簇 E 第一批：把"设备视图"挡在适配层外（1 项） |
| r219 | `ai-censorship/review-20261101-r219.md` | 设备视图的写入路径与会话可见性（缺口 10 → 5）（1 项） |
| r220 | `ai-censorship/review-20261101-r220.md` | 对照表归零：42 条原版路由，缺失 0（1 项） |
| r221 | `ai-censorship/review-20261101-r221.md` | 网络改成单独子页面，与原版对齐（1 项） |
| r222 | `ai-censorship/review-20261101-r222.md` | 四个页签，与原版信息架构对齐（1 项） |
| r223 | `ai-censorship/review-20261101-r223.md` | 设备也做成子页面（1 项） |
| r224 | `ai-censorship/review-20261101-r224.md` | 网络列表的搜索与排序（1 项） |
| r225 | `ai-censorship/review-20261101-r225.md` | 凭据表格补齐原版列（1 项） |
| r226 | `ai-censorship/review-20261101-r226.md` | ACL 从 JSON 文本框变成规则构建器（2 项） |
| r227 | `ai-censorship/review-20261101-r227.md` | 设备接入引导、搜索与实例数（2 项） |
| r228 | `ai-censorship/review-20261101-r228.md` | 临时成员的接入命令与配置复制（1 项） |
| r229 | `ai-censorship/review-20261101-r229.md` | 网络创建向导（1 项） |
| r230 | `ai-censorship/review-20261101-r230.md` | 全局壳补齐三项，语言切换明确没做（1 项） |
| r231 | `ai-censorship/review-20261101-r231.md` | 概览页补齐四张卡与轮询开关（1 项） |
| r232 | `ai-censorship/review-20261101-r232.md` | 语言切换：先做管道，再谈覆盖率（1 项） |
| r233 | `ai-censorship/review-20261101-r233.md` | ACL 统计：从"做不到"变成"通路打通"（2 项） |
| r234 | `ai-censorship/review-20261101-r234.md` | 一个 P1：ACL 构建器写出的策略编译不过（1 项） |
| r235 | `ai-censorship/review-20261101-r235.md` | 把六段临时脚本收成一个可复跑的回归（1 项） |
| r236 | `ai-censorship/review-20261101-r236.md` | 一次 gate 红，但不是回归（1 项） |
| r237 | `ai-censorship/review-20261101-r237.md` | 把网络配置还原成人类能读的样子（1 项） |
| r238 | `ai-censorship/review-20261101-r238.md` | 网络密钥可以重新随机了（1 项） |

台账：`ai-censorship/ledger.md`（308 项：296 FIXED / 9 ACCEPTED / 1 MONITOR / 2 OPEN）。
台账：`ai-censorship/ledger.md`（307 项：295 FIXED / 9 ACCEPTED / 1 MONITOR / 2 OPEN）。
台账：`ai-censorship/ledger.md`（307 项：295 FIXED / 9 ACCEPTED / 1 MONITOR / 2 OPEN）。
台账：`ai-censorship/ledger.md`（306 项：294 FIXED / 9 ACCEPTED / 1 MONITOR / 2 OPEN）。
台账：`ai-censorship/ledger.md`（305 项：294 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（304 项：293 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（304 项：293 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（303 项：292 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（303 项：291 FIXED / 9 ACCEPTED / 1 MONITOR / 2 OPEN）。
台账：`ai-censorship/ledger.md`（301 项：290 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（300 项：289 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（299 项：288 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（299 项：287 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（298 项：287 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（297 项：286 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（296 项：285 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（294 项：283 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（292 项：281 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（291 项：280 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（290 项：279 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（289 项：278 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（288 项：277 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（288 项：277 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（287 项：276 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（286 项：275 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（285 项：274 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（284 项：273 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（283 项：272 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
台账：`ai-censorship/ledger.md`（282 项：271 FIXED / 9 ACCEPTED / 1 MONITOR / 1 OPEN）。
门禁脚本：`scripts/gate.sh`（27 项：24 必跑 + 3 按需——引证扫描、真实客户端矩阵、overlay smoke）。
