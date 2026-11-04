# 前端截图

真机渲染：Playwright 驱动 Chromium 打开本仓库构建出的 `client/dist`，
连接一个真实运行的控制台（SQLite + 一台在线的官方 2.7.0 客户端）。

| 文件 | 路由 | 页面 | 内容 |
|---|---|---|---|
| `01-login.png` | /login | 登录页 | 用户名/密码表单、OIDC 入口由服务端配置决定是否出现 |
| `02-dashboard.png` | / | 概览 | 设备总数 1、在线 1、最近在线 edge-hk-01 |
| `03-devices.png` | /devices | 设备 | 按最后在线排序，含版本、接入地址、状态与操作 |
| `04-networks.png` | /networks | 网络列表 | **只做列表**：每行一个「管理」链接，点进子页面（与原版一致） |
| `05-network-members.png` | /networks/{id}（成员页签） | 网络子页面·成员 | 页头有网络名/方式/虚拟网段，页签「成员 / 设置」；成员显示虚拟 IP、运行中/在线 |
| `05b-network-settings.png` | /networks/{id}（设置页签） | 网络子页面·设置 | 改名、ACL、凭据、高级设置（MTU 1380 / 限速 5000000 / 白名单 *） |
| `06-account.png` | /account | 账户 | OIDC 绑定入口与说明 |
| `07-settings.png` | /settings | 外观 | 主题（跟随系统/浅/深）与背景图地址 |
| `08-admin-users.png` | /admin/users | 用户管理 | 管理员可见 |
| `09-admin-settings.png` | /admin/settings | 系统设置 | 管理员可见 |

## 网络是一个单独的子页面

参照原版的信息架构（`frontend/src/main.ts` 里 `networks/:networkId` → NetworkDetail）：

- 列表页 `/networks` 只负责找到网络，每行一个「管理」链接；
- 详情页 `/networks/{id}` 显示页头（名称 / 方式 / 虚拟网段 / 成员数）与「成员」「设置」页签；
- 详情页是**真路由**：刷新、直接粘贴地址都能打开（浏览器实测过），不再依赖列表页的本地状态。

## 复核结论

- 全程 **0 条浏览器控制台错误、0 个失败请求**。
- 详情页四个断言全部通过：URL 变成 `/networks/{id}`、列表有「管理」链接、成员页签出现 edge-hk-01、设置页签出现高级设置、刷新后仍可打开。
- 高级设置里的 MTU 1380 / 限速 5000000 / 白名单 `*` 与通过 API 写入的值一致。

## 复现

    ./cmd/easytier-webd --http 127.0.0.1:28201 --config-server 127.0.0.1:28202 --web-dir ./client/dist

截图脚本未入库（写死了本机 chromium 路径）；如需固化可加 Playwright 依赖并接成门禁可选一步。
