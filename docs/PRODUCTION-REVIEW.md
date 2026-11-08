# 生产放行评审（五线）

评审对象：EasyTier Web 控制台（本仓库 main @ `69f4615`）
评审依据：`scripts/gate.sh` 最近一次完整运行、`ai-censorship/ledger.md`、`docs/UI-MATRIX.md`、`docs/FEATURE-MATRIX.md`、`scripts/ui-tour.mjs` 实跑结果。

## 结论（先说）

**有条件放行**：自有设备、单租户、可接受"数据面待验证"的前提下可以上线；
**不满足多租户对外服务**，因为 `proxy-rpc` 无方法白名单（F-009）且数据面从未在真机验证（F-283）。

| 线 | 关注 | 投票 |
|---|---|---|
| L1 功能符合性 | 与原版对齐程度、我们多做的部分 | 通过 |
| L2 正确性与数据 | 编译/迁移/双数据库/配置语义 | 通过 |
| L3 测试与回归防线 | gate、测试数量、UI 断言是否在 CI | 有条件通过 |
| L4 运维与可观测 | 部署、备份、端口冲突、指标 | 有条件通过 |
| L5 安全与风险 | 鉴权、越权、RPC 暴露面、密钥 | 有条件通过 |

## L1 · 功能符合性 —— 通过

- 路由对齐：`docs/FEATURE-MATRIX.md` 记录的 23 条缺口已全部补齐，当前 0 缺口；
- 界面六项（ACL 构建器 / 接入引导 / 网络详情 / 创建向导 / 全局壳 / 概览）全部落地并有浏览器断言；
- 我们多做的：TOML 生效配置、网络密钥轮换、实例对端与路由、设备级日志级别、搜索排序、语言切换。

保留意见（不阻塞）：英文只覆盖外壳/导航/概览/登录；设备的位置与公网 IP 未做（需 geoip）。

## L2 · 正确性与数据完整性 —— 通过

- 测试：`go test ./...` 共 **214** 个测试函数；最近一次 gate 全绿；
- 双数据库：gate 里 SQLite 与 PostgreSQL 两条链路都跑（`postgres tests / deploy / api / e2e`）；
- 迁移与冷启动：`backup cold start`、`shared database` 通过；
- 配置语义：F-066（instance_id 与上游不同）与 F-081（未填字段）都有逐项核对记录并引用了上游源码位置。

**但有一个真实的教训必须记在案**：本轮之前 ACL 构建器写出的选择器类型与编译器词表不一致（F-305，P1），
导致整张网络编译失败而界面没有任何提示。它是在"看服务端真正存了什么"时才暴露的。
现在有断言兜住（ui-tour 的 "saved acl policy compiles"），但**那条断言不在 gate 里**。

## L3 · 测试与回归防线 —— 有条件通过

- gate 26 项：backend build/vet/tests、client build/bundle、design contrast/tokens、api contract、
  wire format、client origin/request、gate audit、docs consistency、readme commands、ui states、
  deploy smoke、port conflict、backup cold start、shared database、editor invariants、postgres ×4、real clients。
- UI 断言：`scripts/ui-tour.mjs` **28 条**，本轮实跑全绿。

**条件 1**：ui-tour 需要浏览器，目前**不在 gate 里**，只在人工执行。六项界面能力与 ACL 编译守卫因此没有 CI 兜底。
**条件 2**：F-307 —— `TestUDPSurvivesGarbageAndManyPeers` 用固定 10s 截止，并行负载下会红（单独跑 0.00s 通过）。
一个会随机红的 gate 会训练人们忽略红灯，这比没有断言更危险。

## L4 · 运维与可观测性 —— 有条件通过

- 部署链路有证据：`deploy smoke`、`port conflict`、`backup cold start`、`shared database`；
- 发布：`ci.yml` / `docker-ci.yml`（镜像 tag `:CI`，从不用 latest）/ `release.yml`；
- 健康检查：`/healthz`。

**条件**：本仓库没有指标端点（无 `/metrics`），可观测性只有结构化日志。
生产上"设备批量掉线""下发失败率上升"这类问题，缺指标只能靠人盯日志。
升级/回滚已有证据（r248）：`scripts/upgrade-drill.sh` 用 40 个提交前的二进制在 SQLite 与 PostgreSQL 上各走一遍，前向迁移保留管理员与网络，且旧二进制仍能读迁移后的库。

## L5 · 安全与风险 —— 有条件通过

- 鉴权：会话 Cookie、`requireUser`/`requireAdmin` 包装、登录与改密限流、密码策略、OIDC、
  网络密钥可轮换（本轮新增）、凭据密钥只显示一次；
- 越权：tenant 过滤在存储层，架构测试强制受保护的 GET 与不可变字段。

**条件（多租户时必须处理）**：F-009 —— `proxy-rpc` 没有方法白名单，当前以"仅限自有设备"接受。
一旦开放给互不信任的租户，它就是一条可以代理任意已注册 RPC 的通道。
次要项：密钥轮换没有新旧并存的过渡窗口（已在 r238 记录），轮换会立即踢掉手动接入的设备。

## 放行条件清单

| # | 条件 | 阻塞级别 | 验收方式 |
|---|---|---|---|
| 1 | 数据面验证（F-283） | **CI 已配 tun；本沙箱仍不可** | `.github/workflows/ci.yml` 的 `data-plane` job：拉官方客户端 → `scripts/ci-tun-setup.sh` 建节点并授予 cap_net_admin → `scripts/overlay-smoke.sh`；本沙箱无 sudo，provision 判定 exit 3 |
| 2 | ui-tour 进 CI（r242 已关闭） | **已满足** | `.github/workflows/ci.yml` 新增 `ui-tour` job；空库本地模拟实跑 23 条全绿 |
| 3 | F-307（r243 已关闭） | **已满足** | 8 个 CPU 打满时 ×10 仍绿；race ×10 绿 |
| 4 | `proxy-rpc` 方法白名单（F-009，r244 已关闭） | **已满足** | 只读白名单 + 三条测试；真机三条写方法 403 |
| 5 | 指标端点（F-314，r245 已关闭） | **已满足** | `GET /metrics` 暴露 reconcile 计数与 `easytier_push_failure_ratio`，可选 Bearer |

## 一句话

这不是一个"看起来能跑"的玩具：它有 214 个测试、26 项门禁、双数据库、真实客户端接入、以及一条
能抓住自己的错误的断言链。但它的数据面从未在真机上被证明通过，而这是网络控制台唯一的硬指标。

## 复核（r245 之后）

| 条件 | 结论 | 证据 |
|---|---|---|
| 1 数据面 | 仍阻塞，但从"跑不了"变成"说清为什么跑不了" | `scripts/tun-check.sh` 在 gate 与 CI 先行判定，本机输出 EPERM 原因 |
| 2 ui-tour 进 CI | 关闭 | `ui-tour` job；空库实跑 23 条全绿 |
| 3 F-307 | 关闭 | 8 个 CPU 打满 ×10 仍绿；race ×10 绿 |
| 4 proxy-rpc 白名单 | 关闭 | 只读白名单 + 3 条测试 + 真机 403 验证 |
| 5 指标 | 关闭 | `GET /metrics`，含 `easytier_push_failure_ratio` |

新发现（不阻塞，但会让人误判门禁）：**F-315** —— `smoke-deploy.sh` 的 udp 步骤在整门禁并行时偶发失败，
隔离运行通过，与 F-307 同类（固定等待窗口 + 负载）。

**结论不变**：单租户、自有设备、可接受数据面待验证的前提下可以上线；对外多租户仍需先在有 tun 的机器上把数据面跑通。

## 复核（r248–r251 之后）

| 项目 | 状态 | 证据 |
|---|---|---|
| ACCEPTED 风险 | **清零** | 台账 0 ACCEPTED / 0 MONITOR；每条都有测试或 gate 结果，不再有"看过没问题" |
| 升级 / 回滚 | 已验证 | `scripts/upgrade-drill.sh` 在 SQLite 与 PostgreSQL 上各走一遍：前向迁移保留数据，旧二进制仍能读迁移后的库 |
| 英文覆盖 | 100% | 337/337 键；`scripts/check-i18n.mjs` 进 gate 与 CI，缺键即失败；ui-tour 断言设备页为英文 |
| 门禁 | 全 PASS | 含新步骤 i18n coverage |

**结论不变**：单租户、自有设备、可接受数据面待验证的前提下可以上线；对外多租户仍需先在有 tun 的机器上把数据面跑通（CI 已配 tun，等首跑结果）。

## 使用体验复核（r252）

| 问题 | 之前 | 现在 |
|---|---|---|
| 表单报错 | 直接显示 `virtual_cidr "…" is not a valid CIDR`，字段名与界面不一致 | 服务端给 code/field/value，前端按文案表渲染：「虚拟网段「…」不是合法的网段」 |
| 移动端点按 | 「管理」「文档」16×21px | 55px；ui-tour 断言 ≥40px |
| 空态 | 「尚无设备」「还没有网络」 | 都补了下一步 |

守卫：`scripts/check-errors.mjs` 进 gate 与 CI（组件不得直接渲染传输层英文）；ui-tour 增加三条 UX 断言（中文报错含界面字段名、不含列名、移动端主操作 ≥40px）。

实测：ui-tour **27 条全绿**；gate **23 项 PASS**（本容器已无 PostgreSQL，postgres 相关项如实 SKIP）。

## 交付说明：已验证 / 未验证（r254）

验收一份交付，"范围说清楚了"和"全部验证过"是两件事。下表按 gate 的实际输出逐项列出：**SKIP 不是 PASS，OPEN 不是 FIXED**。

### 已验证（本机跑通）

| 项 | 证据 |
|---|---|
| gate 23 项（22 PASS / 1 SKIP） | `totals: PASS 22 / FAIL 0 / SKIP 1`，跳过项逐条列名 |
| 引用用例 cited tests | `232 cited test(s) ran, 3 skipped for a declared dependency, none failed` |
| 竞态检测 -race | `EASYTIER_RACE=1 ./scripts/gate.sh` 全绿；期间发现并修掉一处真实数据竞争（UDP 监听器上限测试） |
| ui-tour | 29 条断言全绿（错误文案、深浅主题对比度、移动端触达、英文页） |
| 升级 / 回滚 | `scripts/upgrade-drill.sh` 在 SQLite 与 PostgreSQL 上各走一遍 |

### 未验证（本机无法取得证据）

| 项 | 状态 | 为什么没跑 | 谁 / 怎么跑 |
|---|---|---|---|
| postgres tests | **SKIP** | 本容器无 PostgreSQL，apt 源里也没有该包 | 有 PG 的机器：`EASYTIER_TEST_PG_DSN=... EASYTIER_FULL_GATE=1 ./scripts/gate.sh` |
| 3 条 PG 引用用例 | 声明式跳过 | 同上；登记在 `scripts/env-gated-tests.txt`，缺 DSN 时才允许跳过 | 同上；DSN 设置后仍跳过则守卫判红 |
| data plane / overlay smoke | **SKIP** | 本机无 tun（TUNSETIFF: EPERM） | 有 `/dev/net/tun` 的主机：`EASYTIER_OVERLAY_SMOKE=1 ET_BIN=... ./scripts/gate.sh` |
| CI 的 data-plane job | **从未执行** | 工作流已配 tun，但还没有一次运行 | 看 CI 该 job 的首次结果 |
| F-283 数据面语义 | **OPEN** | 同上；另外「停掉 console 后设备 fail-open 还是 fail-closed」没有答案 | 真机两台：跑 overlay smoke + 手工停 console 观察 |

### 明示但不阻塞

| 项 | 状态 | 说明 |
|---|---|---|
| F-323 弱网首屏 | **ACCEPTED** | 400ms/50KB/s 下 300ms 与 900ms 无内容、1500ms 起显示加载壳；head 里的 boot.js 是故意阻塞（防主题白闪），bundle 369KB 需代码分割才能根治 |
| 28/78 条服务端路由无客户端调用 | 已豁免 | 豁免表只拦新增，不代表这些路由被客户端走过 |

### 一句话

**这份交付证明的是：本机能验证的部分都已验证，且每项都留下可重跑的命令；本机不能验证的部分逐条点名，没有一条被算进"通过"。**

### 评审限定语（R-B 要求原样并入，r254）

> 本次可上报的"可验收交付"**不含**：(a) PostgreSQL 生产路径——本环境无 PostgreSQL，gate 该项为 SKIP，未经真库验证；(b) 真 tun 数据面——F-283 仍 OPEN，CI data-plane job **从未执行**，网络控制台的核心硬指标尚无一次真机闭环；因此本交付仅适用于**单租户、自有设备、可接受「数据面待验证」**的场景，不满足对外多租户服务；(c) F-323 弱网首屏为 **ACCEPTED** 风险（400ms/50KB/s 下 1500ms 前无内容），非修复；(d) 前端调用与后端路由的一致性结论只对**非模板字符串、非行尾内联注释**的写法成立——`check-contract.mjs` 的两处扫描器漏洞仍未修，该守卫当前证明力弱于其命名。
>
> 一句话：**范围说清楚了，我放行；"已验证"这三个字不许跨过上面四道边界。**
