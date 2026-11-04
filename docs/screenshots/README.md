# 前端截图

真机渲染，不是设计稿：用 Playwright 驱动 Chromium 打开本仓库构建出的 `client/dist`，
连接一个真实运行的控制台（SQLite + 一个正在线的官方 2.7.0 客户端）。

| 文件 | 路由 | 页面 | 内容 |
|---|---|---|---|
| `01-login.png` | /login | 登录页 | 用户名/密码表单、OIDC 入口由服务端配置决定是否出现 |
| `02-dashboard.png` | / | 概览 | 设备总数 1、在线 1、最近在线 edge-hk-01 |
| `03-devices.png` | /devices | 设备 | 按最后在线排序，含版本、接入地址、状态与操作 |
| `04-networks.png` | /networks | 网络列表 | 网络卡片 + 新建表单（名称/虚拟网段） |
| `05-network-open.png` | /networks（展开） | 网络展开 | 成员列表（含虚拟 IP、运行中/在线）、网络设置、ACL、凭据、高级设置 |
| `05b-network-advanced.png` | /networks（展开） | 高级设置面板 | MTU 1380、限速 5000000 bps、白名单 *、功能开关 |
| `06-account.png` | /account | 账户 | OIDC 绑定入口与说明 |
| `07-settings.png` | /settings | 外观 | 主题（跟随系统/浅/深）与背景图地址 |
| `08-admin-users.png` | /admin/users | 用户管理 | 管理员可见 |
| `09-admin-settings.png` | /admin/settings | 系统设置 | 管理员可见 |

## 复核结论

- 全程 **0 条浏览器控制台错误、0 个失败请求**（脚本记录了 console error 与 >=400 的响应）。
- 高级设置面板里的输入值是**从后端读回来的**：MTU 1380、限速 5000000、白名单 `*`，与通过 API 写入的值一致。
- 成员列表显示虚拟 IP、运行中/在线状态，说明编译下发的链路在界面上是通的。

## 复现方式

    ./cmd/easytier-webd --http 127.0.0.1:28201 --config-server 127.0.0.1:28202 --web-dir ./client/dist

然后用任意无头浏览器登录 `root-owner` 即可。截图脚本未入库（依赖本机 chromium 路径），
如需固化，可把 Playwright 作为开发依赖加入并接成门禁的可选一步。
