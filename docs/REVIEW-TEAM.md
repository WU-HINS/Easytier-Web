# 代码审核团（常设）

对齐 HiDNS `root.ai.md`「代码审核团」三节。本文件定义**常设角色**、**每轮动作**、**证据标准**与**跟进台账**。
审核团不是一次性动作：每一轮结束都要更新 `ai-censorship/ledger.md`，未闭环的问题不允许静默消失。

## 0. 运行方式

两种模式并存，按轮次选择：

| 模式 | 做法 | 何时用 |
|---|---|---|
| 主线自审 | 由主线依次扮演 R-A..R-F，逐个维度取证 | 每轮默认 |
| 智能体团队 | 用平台 `spawn_teammate` 同时起六个成员，各自只读、禁构建、限时 8 分钟，结论带 文件:行号 | r134 起，每若干轮一次 |

团队模式下主线只做三件事：复核每条结论、修可修的、其余记 OPEN。
## 1. 角色

六个角色独立取证、独立打分，互相**不得替对方结论背书**。同一轮里若两个角色结论冲突，以「有可复现证据」的一方为准，另一方必须补齐证据或撤回。

### R-A 前端美观（Design）
- 检查：设计 token 一致性（禁止硬编码 px/色值）、黄金比例阶梯、对比度（正文 ≥4.5:1，控件边界 ≥3:1）、
  空/错/载入三态、窄屏降级、键盘可达与 focus ring。
- 证据：源码行号 + 构建产物体积 + 页面截图（无头浏览器或手工）。
- 放行条件：无硬编码单位；三态齐全；P0=0。

### R-B 完整从0部署（Deploy）
- 检查：空机器 → 依赖 → 建库 → 起进程 → 初始化 → 登录 → 接入设备，全程只用文档；
  反向验证：少一个环境变量、端口冲突、只读目录时是否给出可操作报错。
- 证据：`scripts/smoke-deploy.sh` 的完整输出。
- 放行条件：脚本一次通过；失败路径有明确错误码。

### R-C 极端碰撞（Collision）
- 检查：并发初始化、并发删除、并发改配额、重复绑定、超长/非法输入、时钟偏移、时区、空表与巨表。
- 证据：并发用例（≥8 协程）+ 断言"恰好一个成功"。
- 放行条件：无重复副作用；错误可区分（409 vs 400）。

### R-D 红蓝攻防（RedTeam/BlueTeam）
- 红队：枚举攻击面并实际打（会话固定、CSRF、点击劫持、开放重定向、SSRF、响应头缺失）。
- 蓝队：逐条给出阻断点与日志证据（`[Authz] [DENY]` 之类）。
- 放行条件：每个攻击面要么被阻断，要么登记为已接受风险并写明补偿措施。

### R-E 漏洞渗透（Pentest）
- 检查：未初始化绕过、IDOR、路径穿越、token 重放、暴力破解、枚举用户名、错误信息泄露。
- 证据：原始请求 + 响应码 + 响应体。
- 放行条件：P0=0；P1 有明确修复计划。

### R-F 越权（Authz）
- 检查：角色矩阵逐条（匿名 / 普通用户 / 管理员 × 全部端点），跨租户访问，管理员自身保护。
- 证据：矩阵表 + 自动化用例。
- 放行条件：矩阵 100% 符合预期。

## 2. 每轮动作（多轮多步）

1. **准备**：从 ledger 取未闭环项 + 本轮覆盖面。
2. **取证**：跑门禁 + 针对性攻击/并发/部署脚本，**先取证后下结论**。
3. **打分**：P0 阻断发布；P1 发布前必修；P2 登记可延后。
4. **修复**：P0/P1 当场修，必须带回归用例；不能当场修的写清阻塞原因。
5. **留痕**：`ai-censorship/review-<YYYYMMDD>-r<N>.md` + 更新 `ledger.md`（ID、状态、证据、验证方式）。
6. **复评**：下一轮必须先复验上一轮的 P0/P1 是否真的闭合。

## 2.5 守卫扫描（R-F / R-E 每轮可做）

把 `internal/httpapi/*.go` 里所有 `writeError(...)` 的文案抽出来，与 `_test.go` 里的断言比对：**列出"守卫存在但没有用例证明"的位置**，
优先给越权/认证相关的补用例（r46 的 OIDC 绑定、r47 的跨站写与禁用账户都是这样挖出来的）。

注意：不是每条文案都需要断言文本，重点是**行为**有没有被测到。

## 3. 门禁（每轮必跑）

每轮只跑**一个入口**，避免章程与实际执行的命令漂移：

```bash
./scripts/gate.sh                              # SQLite
EASYTIER_TEST_PG_DSN=... ./scripts/gate.sh     # 追加 PostgreSQL
```

它按顺序执行并逐项 PASS/FAIL：

| 项 | 内容 |
|---|---|
| backend build | `go build ./...` |
| backend vet | `go vet ./...` |
| backend tests | `go test -count=1 ./...` |
| client build | `cd client && npm run build`（tsc -b + vite） |
| client bundle | `node scripts/check-bundle.mjs`（index.html 引用都存在、bundle 无 eval、大小合理） |
| design contrast | `node scripts/check-contrast.mjs`（WCAG，浅/深 10 组） |
| design tokens | `node scripts/check-design-tokens.mjs`（禁止硬编码色值/px） |
| api contract | `node scripts/check-contract.mjs`（前端调用↔后端路由比对） |
| wire format | `node --experimental-strip-types scripts/check-wire.mjs`（ipv4/uuid 编码对齐上游 proto） |
| client origin | `node --experimental-transform-types scripts/check-client-origin.mjs`（回调地址由浏览器生成、非安全源不启用绑定） |
| client request | `node --experimental-transform-types scripts/check-client-request.mjs`（同源凭据、错误映射、204 无体） |
| gate audit | `./scripts/audit-gates.sh`（在仓库副本上逐个破坏 12 条检查的被测物（9 条静态 + 3 条 Go 不变量），任何一条不红即失败） |
| docs consistency | `node scripts/check-docs.mjs`（章程/门禁/台账/评审记录互相引用一致） |
| readme commands | `node scripts/check-readme.mjs`（README 里的参数/路由/脚本/环境变量都存在） |
| ui states | `node scripts/check-ui.mjs`（页面加载/错误分支、控件可访问名、fetch 只能走 lib/http.ts、禁止 dangerouslySetInnerHTML） |
| i18n coverage | `scripts/check-i18n.mjs`：中文表里每个键都必须有英文，缺一个即失败 |
| feature layout | `scripts/check-features.mjs`：调用服务端的 feature 必须有 api.ts 与 use-*.ts，纯本地页须写 local-only 说明 |
| error contract | scripts/check-errors.mjs：组件不得直接渲染传输层英文，错误文案表必须存在 |
| deploy smoke | `bash scripts/smoke-deploy.sh`（空库→初始化→登录→注册→头→WS） |
| port conflict | `./scripts/check-port-conflict.sh`（端口被占用必须非零退出且不得声称 listening） |
| backup cold start | `./scripts/check-backup-cold-start.sh`（开启备份的冷启动必须成功并留下快照） |
| shared database | `./scripts/check-shared-database.sh`（两个实例共库时必须播报降级模式） |
| editor invariants | `node scripts/check-editor-invariants.mjs`（保存后推进基线、空 patch 不发、无改动禁用保存） |
| real clients | `./scripts/real-instance-matrix.sh`（官方 2.4.0 / 2.5.0 / 2.6.4 / 2.7.0 逐个接入：注册、并网、下发；按需，需要下载的发行包） |
| overlay smoke | `./scripts/overlay-smoke.sh`（两台受管实例互相可见；数据面缺 tun 时以退出码 3 记为 SKIP；按需） |
| data plane | 先跑 `scripts/tun-check.sh`：缺驱动/设备节点/CAP_NET_ADMIN 时打印具体原因并 SKIP（绝不当作通过）；有能力时才继续 overlay smoke |
| postgres tests | 设置了 `EASYTIER_TEST_PG_DSN` 才运行，否则显式 SKIP |
| postgres deploy | 同一 DSN 下把完整部署冒烟再在 PostgreSQL 上跑一遍（重置 schema） |
| postgres api | 同一 DSN 下把整个 REST 用例套件在 PostgreSQL 的私有 schema 里再跑一遍 |
| postgres e2e | 同一 DSN 下把对账/配额/会话等端到端场景在 PostgreSQL 的私有 schema 里再跑一遍 |
| cited tests | `./scripts/check-citations.sh`（可选：设置 EASYTIER_FULL_GATE=1 才跑；账本引用的用例必须真的运行、不得跳过） |

race 是修饰符而不是步骤：设 `EASYTIER_RACE=1` 会给测试步骤加 `-race`（耗时 ×2~3，建议每 3 轮跑一次）。

任何一条红，本轮直接判定不通过，不允许"下一轮再说"。
脚本本身也纳入评审对象：新增门禁项必须同时更新本表与 `scripts/gate.sh`。

## 4. 上报与淘汰

- 连续两轮出现同类 P0 → 上升到"架构缺陷"，暂停新功能，先补结构性防线（例如本轮把主键来源写进规范）。
- 审核团自身误判（把 P0 判成 P2）一次记过，两次则该角色本轮结论作废重审。
- 所有结论必须能被第三方用文档里的命令复现；不能复现的结论一律降级为 P2。

## 5. 定期深检（不每轮跑，但必须定期跑一遍）

每次深检的时间、命令与结论记录在 `docs/DEEP-CHECKS.md`，由 `scripts/check-docs.mjs` 校验其引用的轮次真实存在。

门禁每次都会跑，下面这些因为耗时或依赖环境，按需执行：

| 检查 | 命令 | 期望 |
|---|---|---|
| 全量数据竞争 | `EASYTIER_RACE=1 ./scripts/gate.sh` | 全部步骤全绿（含 postgres api / e2e） |
| PostgreSQL 浸泡 | `EASYTIER_E2E_PG=<dsn> EASYTIER_SOAK_ROUNDS=40 go test -race -run TestSoakDeviceChurn ./internal/e2e/` | 通过且耗时随轮数线性 |
| 线格式模糊 | `bash scripts/fuzz.sh` | 无 panic、无越界 |
| 有状态序列 | `go test -count=20 -run TestStatefulPacketSequencesStayBounded ./internal/rpc/` | 20 次全过（防 flaky） |

最近一次结果记录在对应的 `ai-censorship/review-*.md` 里。
