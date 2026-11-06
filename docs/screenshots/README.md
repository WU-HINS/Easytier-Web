# 前端截图

真机渲染：Playwright 驱动 Chromium 打开本仓库构建出的 `client/dist`，连接一个真实运行的控制台
（SQLite + 一台在线的官方 2.7.0 客户端）。

| 文件 | 路由 | 内容 |
|---|---|---|
| `01-login.png` | /login | 登录页 |
| `02-dashboard.png` | / | 概览 |
| `03-devices.png` | /devices | 设备 |
| `03-devices.png` | /devices | 设备列表（每行一个「详情」链接） |
| `04-networks.png` | /networks | 网络列表（搜索框 + 计数 + 每行「管理」） |
| `04a-networks-filtered.png` | /networks（搜索后） | 搜索命中只剩一条，计数随之变化 |
| `05-network-members.png` | /networks/{id} · 成员 | 子页面·成员页签 |
| `05a-network-acl.png` | /networks/{id} · 访问控制 | 子页面·访问控制页签（ACL JSON 编辑器） |
| `05b-network-credentials.png` | /networks/{id} · 凭据 | 子页面·凭据页签：表头为 凭据 ID / ACL 组 / 允许中继 / 可复用 / 过期 / 操作 |
| `05c-network-settings.png` | /networks/{id} · 设置 | 子页面·设置页签（改名 + 高级设置） |
| `06-account.png` | /account | 账户与 OIDC 绑定 |
| `07-settings.png` | /settings | 外观 |
| `08-admin-users.png` | /admin/users | 用户管理 |
| `09-admin-settings.png` | /admin/settings | 系统设置 |

## 网络：单独子页面 + 四个页签（对齐原版）

原版 `frontend/src/main.ts` 的路由是 `networks/:networkId` → NetworkDetail，页签为
members / acl / credentials / settings。我们现在是同样的结构：

- `/networks` 只做列表，每行一个「管理」链接；
- `/networks/{id}` 是独立路由，页头显示名称 / 方式 / 虚拟网段 / 成员数；
- 四个页签各自独立组件：`network-members`、`network-acl-panel`、
  `network-credentials-panel`、`network-settings`（含高级设置）。

## 复核结论

- 浏览器实测：页签名依次为「成员 / 访问控制 / 凭据 / 设置」，**每个页签只显示自己的内容**：
  成员页出现 edge-hk-01、访问控制页只出现 ACL 编辑器、凭据页只出现签发按钮、设置页出现名称与高级设置。
- 刷新详情地址后仍可打开（真路由，不是本地状态）。
- 全程 0 console error、0 失败请求。

## 复现

    ./cmd/easytier-webd --http 127.0.0.1:28201 --config-server 127.0.0.1:28202 --web-dir ./client/dist

## 设备：也是单独子页面

与原版 `deviceList/device/:deviceId` 一致：列表只负责找到设备，
`/devices/{id}` 显示页头（名称 / machine id / 在线 / 版本 / 接入地址 / 最后在线）与「所属网络」列表
（网络名、虚拟 IP、运行中标记，并可跳到网络子页面）。
浏览器实测：URL 变为 `/devices/{id}`、页头与网络行都渲染、刷新后仍可打开、0 console error。
## 一次跑完全部断言

截图与断言现在由脚本产出，不再靠临时命令：

    PLAYWRIGHT_CORE=/path/to/playwright-core/index.mjs node scripts/ui-tour.mjs [base-url] [截图目录]

脚本会逐项断言（登录、概览四张卡与轮询开关、接入命令、设备搜索与实例列、网络搜索、创建向导的四种方法、
四个页签、ACL 策略可编译、临时凭据命令、封禁页、改密页、语言切换），任何一项失败即退出码非 0，并同时刷新本文列的截图。

说明：`scripts/ui-tour.mjs` 依赖 `playwright-core` 与本机 chromium，**不在 gate 里**（gate 需要无浏览器环境可跑），
与 `real clients` 一样按需执行。

ui-tour 现在也在 CI 里跑：空库起控制台 → 自建网络 → 断言 → 删除。
