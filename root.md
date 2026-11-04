# 项目开发规范（root.md）

> 本文件是本项目唯一的规范总纲，与 HiDNS 的 `root.ai.md` 同构。
> 分工：**root.md 说"必须怎么做"**，`docs/GOALS.md` 说"验收口径与评审历史"，
> `docs/REVIEW-TEAM.md` 说"评审团如何运作与门禁明细"，`ai-censorship/ledger.md` 说"逐条发现的状态"。
> 四份文件不重复叙述同一件事；root.md 里的每条规则尽量标注**它在何处被机器检查**。

## 1. 项目概述

- EasyTier 控制台（easytier-console）：EasyTier 网络的管理面板，替代上游 Rust 版的 web 控制面。
- 后端 Go 1.23（`cmd/easytier-webd`），前端 React 18 + Vite + Tailwind（`client/`）。
- 与设备之间走 **不可修改** 的 EasyTier 线协议（`internal/wire`、`internal/rpc`）。
- 数据库双方言：SQLite（默认）与 PostgreSQL，schema 完全声明式。

### 1.1 配置来源与优先级

| 来源 | 作用 | 优先级 |
|---|---|---|
| 环境变量 `EASYTIER_DB_*`、`EASYTIER_LOG_LEVEL`、`EASYTIER_SETUP_TOKEN` | 启动参数 | 始终生效（没有其它来源） |
| 环境变量 `EASYTIER_OIDC_*` | 首个 provider | **低于**后台设置：后台一旦启用即以后台为准；后台未启用时作为兜底 |
| 后台设置（settings 表） | 运行期配置 | 高于同名环境变量 |

两者都在时，日志会写明当前生效的来源（`OIDC [READY]` 或 `OIDC [SEED]`）。
## 2. 不可变约束

| 约束 | 含义 | 破坏后果 |
|---|---|---|
| 线协议兼容 | 帧格式、字段序、UUID 大端、`method_index` 从 1 开始 | 现网设备立即失联 |
| 配置只来自环境变量 | `EASYTIER_DB_*`、`EASYTIER_LOG_LEVEL` 等；不读配置文件 | 部署方式分叉 |
| schema 只有声明式 | 唯一事实来源是 `internal/db/dsm`，仓库里没有 `.sql` 迁移 | 两个方言漂移 |
| 单管理员 | 由数据库部分唯一索引保证，不是应用层检查 | 并发下出现两个 admin |
| 业务代码不碰数据库 | 只允许 import `internal/db/bal` | 分层失效、SQL 散落 |
| 设备通道复用端口 | WS 控制面在 `/@up/<用户名>`，与面板同端口 | 反向代理配置失效 |

## 3. 代码审核团

评审团由六个角色组成，每轮至少覆盖其中两个，六轮内六个维度各覆盖一次：

| 角色 | 维度 | 主要手段 |
|---|---|---|
| R-A | 前端美观 | token / 对比度 / 三态 / 可访问名 |
| R-B | 完整从 0 部署 | 空库走查、冒烟、备份与恢复 |
| R-C | 流程 | 校验器自身、文档一致性、评审记录完整性 |
| R-D | 极端碰撞 | 并发、时序、时钟、重复提交、越权并发 |
| R-E | 红蓝攻防 / 漏洞渗透 | 攻击面枚举、真实请求、负向矩阵 |
| R-F | 越权 | 角色矩阵、跨租户 IDOR、撤销语义 |

规则：

1. 每一轮产出一份记录：`ai-censorship/review-YYYYMMDD-rNN.md`，写明范围、发现、复现证据、结论。
2. 发现必须编号进 `ai-censorship/ledger.md`，状态只有 `OPEN / FIXED / ACCEPTED / MONITOR` 四种，行只改状态不删除。
3. 评分为 P0（阻断发布）/ P1（发布前必修）/ P2（可延后但必须登记）。
4. **FIXED 必须指名一条会失败的用例或一次可复现的实测**；说不清的降级为 MONITOR。
5. 连续出现 P0 或多次 P1，评审团有义务上报；评审团自身多次误判，项目方有权更换评审。
6. 每轮结束必须让门禁全绿才能结轮。

## 4. 提交相关

- 不擅自 push，不擅自改写历史。
- 根目录只允许 `README.md` 与本文件 `root.md`；其余文档一律放 `docs/`，评审记录放 `ai-censorship/`。
- 提交信息：第一行不超过 72 字符的主题，空行后写变更内容、影响面、验证方式。

## 5. 代码规范

### 5.1 分层与依赖方向

    httpapi / session / central      （业务层）
        → bal                        （唯一可用的数据接口）
            → dsm                    （声明式 schema + 对齐器）
                → dal                （驱动抽象、方言、查询构造）

- 只有 `internal/db/dal` 可以 import `database/sql`（`TestOnlyTheDataLayerImportsDatabaseSQL`）。
- 只有 `internal/logging` 可以写日志（`TestBusinessCodeUsesTheProjectLogger`）。
- 业务代码不得直接操作数据库（`TestBusinessCodeNeverTouchesTheDatabaseInternals`）。

### 5.2 错误处理

- 约束冲突在 **数据层** 翻译成哨兵错误（`ErrUsernameTaken`、`ErrNetworkExists`、`ErrQuotaExceeded`、`ErrMemberExists`）；
  handler 只认哨兵，**永远不匹配驱动文本**（`TestHandlersDoNotKnowDriverErrors`）。
- 对外文案不泄露表名、列名、SQL、驱动信息；内部原因用日志。
- 4xx 表达"你的输入/状态不允许"，5xx 只留真正无法归类的失败。

### 5.3 并发与不变量

- **能被约束表达的，就用约束表达**：唯一性 → 唯一索引（含部分唯一索引）；缺省值 → 默认值。
- 数量上限这类无法用索引表达的，必须在**同一事务内**取串行点（行锁）再判断（配额即如此）。
- 禁止"先读一个数、再据此决定要不要写"的裸实现；确实需要时，登记进 `guardThenInsertReviewed` 并写明为什么安全（`TestGuardThenInsertIsReviewed`）。
- 并发路径必须有双方言用例，SQLite 的单写者会掩盖问题。

### 5.4 日志

- 格式：`时间 级别 [主模块] [子模块] [函数] [L行号] ["标签"] 内容`（`TestFormatMatchesTheDocumentedShape`）。
- 级别由 `EASYTIER_LOG_LEVEL` 控制，默认 `INFO`。
- **密钥不入日志**：日志调用里不得出现 secret / password / token / hash / verifier 这类标识符（`TestLogsNeverCarrySecrets`）。

### 5.5 密钥与敏感数据

- 网络密钥、OIDC client secret、凭证密钥**不出现在任何响应里**（`TestNetworkSecretNeverLeavesTheServer`、`TestSettingsNeverReturnTheClientSecret`、`TestNoEndpointEchoesKnownSecrets`）。
- 凭证密钥只在创建时返回一次，之后不可恢复。
- 前端不得把密钥写入 localStorage、URL 或日志。

### 5.6 数据库

- 表、索引、约束全部写在 `internal/db/dsm`；新增字段必须同时考虑两个方言。
- 声明的每个索引都必须在引擎里真实存在（`TestDeclaredIndexesExistInTheEngine`）。
- 启动对齐顺序固定：**快照 → DDL → 起服务**；快照失败即中止（`TestBackupRunsBeforeSchemaChange`）。
- 破坏性变更不自动执行：旧库只报告，DryRun 只输出计划（`TestLegacyDatabaseIsOnlyReported`、`TestDryRunReportsWithoutApplying`）。
- 设置项读改写必须走修订号 CAS（`TestSettingsRevisionConflict`）。

### 5.7 HTTP 接口

- 路径前缀 `/api/v1`；JSON 字段 `snake_case`；错误体固定 `{"code": "...", "message": "..."}`。
- 关键错误码：`setup_required`（未初始化）、`already_initialized`、`quota_exceeded`、`member_exists`、`network_exists`、`user_has_online_instances`。
- 客户端调用与服务端路由必须双向一致（`scripts/check-contract.mjs`）。
- 跨站写请求必须过 origin 守卫；状态变更一律不接受 GET。

### 5.8 前端

- 颜色与尺寸只能来自设计 token；φ = 1.618034，几何阶梯为 `8·φ^((n-3)/2)`（`scripts/check-design-tokens.mjs`）。
- **用户可见文字只能来自 `client/src/copy/zh-CN.ts`**：组件里出现中文字面量即失败（`scripts/check-ui.mjs` 的 CJK 规则，`scripts/cjk-baseline.json` 现为空）；
  带变量的文案写成 copy 里的函数（如 `copy.adminSettings.invalidCount(n)`），不要在组件里拼字符串。
- 正文对比度不低于 WCAG AA（`scripts/check-contrast.mjs`）。
- 有循环动画的样式表必须提供 `prefers-reduced-motion: reduce` 的静止版本（`scripts/check-ui.mjs` 静态检查）。
- 每个数据页必须有 loading / error / empty 三态，交互元素必须有可访问名（`scripts/check-ui.mjs`）。
- 动态出现的错误文案必须是 live region（`role="alert"`），只染红不会被读屏播报（`scripts/check-ui.mjs`）。
- 主壳层必须有跳转链接直奔 `#main-content`，`<main>` 必须带该 id（`scripts/check-ui.mjs`）。
- 全站只有一个网络出口：`client/src/lib/request.ts`；同源凭据、错误映射、204 语义都在那里（`scripts/check-client-request.mjs`）。
- OIDC 回调地址由浏览器生成，非安全源不启用绑定（`scripts/check-client-origin.mjs`）。

### 5.9 测试规范

| 规则 | 说明 |
|---|---|
| 命名 | `Test<主体><行为><期望>`，能在失败信息里读懂意图 |
| 可红 | 新增守卫必须证明"把被测物破坏后它会变红"，否则不算守卫 |
| 负向自测 | 每条修复附一次变异验证，记录在评审文件里 |
| 双方言 | 数据层与 API 用例在 SQLite 与 PostgreSQL 各跑一次 |
| 不空转 | `scripts/audit-gates.sh` 会在副本上破坏 14 处被测物，任一检查不变红即失败 |

### 5.10 文档规范

| 文件 | 职责 | 机器校验 |
|---|---|---|
| `README.md` | 人读的安装、配置、用法 | `scripts/check-readme.mjs`（参数/路由/脚本/环境变量 + 请求体字段） |
| `root.md` | 本规范总纲 | `scripts/check-docs.mjs`（门禁项齐全、引用的用例存在） |
| `docs/GOALS.md` | 目标、验收清单、评审历史 | `scripts/check-docs.mjs`（引用用例存在、轮次连续） |
| `docs/REVIEW-TEAM.md` | 评审团运作与门禁明细 | `scripts/check-docs.mjs`（门禁表与脚本一致） |
| `ai-censorship/ledger.md` | 逐条发现与状态 | `scripts/check-docs.mjs`（状态词表、编号连续） |
| `ai-censorship/review-*.md` | 每轮记录 | `scripts/check-docs.mjs`（连续编号、被 GOALS 引用） |
| `docs/DEEP-CHECKS.md` | 深检记录（数据竞争 / 浸泡 / 模糊长跑） | `scripts/check-docs.mjs`（引用的轮次必须有评审记录、不得空表） |
| `docs/PROTOCOL.md` | 线协议与 RPC 方法索引（对照上游 proto） | `scripts/check-wire.mjs` 校验编码，文档本身靠评审复查 |
| `docs/DESIGN.md` | 前端设计系统（φ 布局、全局背景、组件约定） | `scripts/check-contrast.mjs` / `scripts/check-design-tokens.mjs` |
| `docs/REWRITE-PLAN.md` | 重写方案、与 2.7.0 的差异与风险 | 评审时人工核对（见待办 §8） |

写作要求：不写"大概""可能"；结论先行；有数字就给数字；没做的要写"没做"。

### 5.11 路由与越权不变量

四条守卫都从 `internal/httpapi/router.go` 源码解析路由，不依赖手写清单——
新增一条路由时，它们自动把新路由纳入检查。

| 不变量 | 守卫 | 破坏后的后果 |
|---|---|---|
| 管理路由必须套 `requireAdmin` | `TestAdminRoutesRequireTheAdminWrapper` | 普通用户可直接调用管理接口 |
| 读路由必须套 `requireUser`（四条公开路径除外） | `TestReadRoutesRequireASession` | 未登录即可读租户数据 |
| 所有写路由必须过跨站守卫 | `TestCrossOriginGuardCoversEveryMutatingRoute` | 任意站点可代用户发起写请求 |
| 租户表的每条语句必须带 `user_id` | `TestTenantTablesAreAlwaysQueriedWithAUser` | 跨租户读取（IDOR） |

新增路由时若忘了外壳，门禁会在 `backend tests` 直接变红，并在报错里给出具体路径。

### 5.12 响应安全头与缓存

| 头 | 值 | 何时 |
|---|---|---|
| Content-Security-Policy | 见 `internal/httpapi/security_headers.go` | 始终 |
| Strict-Transport-Security | `max-age=15552000`（不含 includeSubDomains） | 仅 TLS 或 `EASYTIER_SECURE_COOKIES=1` |
| Cache-Control / Pragma | `no-store` / `no-cache` | 仅 `/api/` 前缀 |
| X-Frame-Options / nosniff / Referrer-Policy / COOP | 固定值 | 始终 |

对应用例：`TestSecurityHeaders`、`TestHSTSOnlyOnSecureTransport`、`TestApiResponsesAreNeverCached`。

## 6. 门禁（25 项，第 22 项按需）

一次全量跑：`./scripts/gate.sh`；加 `EASYTIER_RACE=1` 追加数据竞争检测。

| # | 项 | 守护什么 |
|---|---|---|
| 1 | backend build | 能编译 |
| 2 | backend vet | 静态检查 |
| 3 | backend tests | 后端行为 |
| 4 | client build | 前端能构建 |
| 5 | client bundle | 产物引用完整、无 eval、体积合理 |
| 6 | design contrast | 对比度 |
| 7 | design tokens | token 与 φ 阶梯 |
| 8 | api contract | 客户端调用 ↔ 服务端路由 |
| 9 | wire format | 与协议定义一致 |
| 10 | client origin | 回调地址与安全源 |
| 11 | client request | 传输契约 |
| 12 | gate audit | 以上检查都"能红" |
| 13 | docs consistency | 文档与代码一致 |
| 14 | readme commands | README 可执行 |
| 15 | ui states | 三态与可访问名 |
| 16 | deploy smoke | 从 0 部署走查（SQLite） |
| 17 | port conflict | 端口被占用时必须失败退出 |
| 18 | postgres tests | 数据层双方言 |
| 19 | postgres deploy | PostgreSQL 上的部署走查 |
| 20 | postgres api | REST 套件在 PostgreSQL 上重跑 |
| 21 | postgres e2e | 端到端场景在 PostgreSQL 上重跑 |
| 22 | cited tests | 可选（`EASYTIER_FULL_GATE=1`）：账本引用的用例必须真的运行、不得跳过 |
| 23 | backup cold start | 开启备份的冷启动必须成功，并在改动 schema 前留下快照 |
| 24 | shared database | 两个实例共库时必须播报降级模式 |
| 25 | editor invariants | 成员编辑器必须保留：保存后推进基线、空 patch 不发、无改动禁用保存 |

## 7. 版本发布流程

1. 门禁 20 项全绿，且 `ai-censorship/ledger.md` 无 OPEN 项。
2. 更新 `docs/GOALS.md` 的验收清单与评审历史。
3. 版本号采用 `vMAJOR.MINOR.PATCH`：协议或数据不兼容升 MAJOR，新增能力升 MINOR，其余 PATCH。
4. 发布说明列出：新增能力、破坏性变更、已知限制（含未实现项）。

## 8. 待办与已知边界

- Docker / Compose 参考编排未在实机验证（当前环境无 daemon）。
- 多副本共库可运行但会重复上报设备，未做租约。
- 有状态 fuzz 目前是固定种子的确定性序列，未做长跑。
- 冒烟中的 reconnect（重连补齐）分支由 e2e 用例覆盖，未在冒烟里单独做"打红"验证。

## 9. 达成度自评（r154）

目标里的六个维度不是口号，各自有账本条目与可执行检查兜着。截至本轮：

| 维度 | 账本条目 | 已修 | 主要证据形态 |
|---|---|---|---|
| R-A 前端美观 | 40 | 38 | 对比度 9 组配对、设计令牌扫描、三态/可访问名/role=alert/跳转链接静态检查、成员编辑器三不变量 |
| R-B 完整从 0 部署 | 66 | 64 | 冷启动走查（SQLite + PostgreSQL）、备份冷启动、端口冲突、共享库降级播报、readme 双向核对 |
| R-C 碰撞与流程 | 49 | 46 | 17 条"必须可红"的审计用例、文档一致性、引用证据扫描 |
| R-D 极端碰撞 | 38 | 38 | 并发用例（配额/注册/设置/成员/会话撤销）、竞态检测、UDP 边界与脏包 |
| R-E 漏洞渗透 | 28 | 27 | 秘密不外泄扫描、500 不回显、CSP/HSTS/缓存头、OIDC cookie 属性 |
| R-F 越权 | 39 | 39 | 全端点"他人 id"行为矩阵、租户过滤、单管理员约束、IDOR 用例 |
| **合计** | **260** | **252** | 0 OPEN / 8 ACCEPTED / 0 MONITOR |

严重度分布：P0 3 条、P1 57 条、P2 182 条、P3 4 条——**全部有归属**，没有一条消失在对话里。

### 9.1 尚存的边界（不假装完成）

| 事项 | 状态 |
|---|---|
| Docker / Compose 实机验证 | MONITOR：本环境无 docker daemon，镜像层（nonroot + 根属主卷）未能实跑 |
| 多副本共库 | MONITOR：可运行但会重复上报设备，未做租约 |
| instance_id 语义与上游差异 | MONITOR：语义分歧已记录，未与上游对齐 |
| UDP 乱序 | 已测：同事务两片按 1→0 到达仍只执行一次（TestReorderedPiecesOverUDPStillMerge） |
| UDP 源端口重绑定 | 已测：重绑定对端拿到独立隧道且帧不串（TestUDPPeerThatChangesSourcePortGetsANewTunnel） |
| 跨公网 NAT 的地址映射与穿透 | 未实测：需要第二台机器与一个真实 NAT，本环境无此条件 |
| 真实 EasyTier 客户端实例上报 | 未实测：本环境无 Rust 工具链与 ET 二进制；已提供 scripts/real-instance-check.sh（无客户端时以退出码 2 明示"未验证"） |
| 长时间压测 / fuzz 长跑 | 未做：目前是固定种子确定性序列 |

### 9.2 这套流程本身的可信度

判定"已完成"不看我说了什么，而看四件事能否同时成立：

1. `scripts/gate.sh` 23 项全绿（含 16 条可红审计、1 条按需的引用证据扫描）；
2. 账本每条 FIXED 都指向一个**存在**的用例，`check-docs` 机械核对引用；
3. 每条守卫都用探针证明过"该红的时候会红"；
4. 团队结论同样要复核——r150 就有一条"必红"的判断被门禁否证。

## 10. 持续集成

| 工作流 | 触发 | 产物 | 关键约束 |
|---|---|---|---|
| `ci` | push / PR | vet、全量测试（含 PostgreSQL 服务）、客户端构建、体检脚本 | 测试不允许静默跳过：CI 里显式提供 PG DSN |
| `docker-ci` | 每次 push | `:CI` + `:ci-<sha>`（多架构） | 不推 `latest`；latest 只属于发版 |
| `release` | `v*` 标签 | 双架构二进制、多架构镜像、Release | 二进制与镜像同源于同一个 commit |

**原生构建是硬约束**：每个构建 job 先断言 `uname -m` 与矩阵架构一致，不一致即失败。
这样不会出现"用 QEMU 交叉模拟、镜像能起但行为不可信"的产物——与本项目
"配置即承诺，兑现不了就失败"是同一条规则。
