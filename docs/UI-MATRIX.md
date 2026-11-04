# 界面功能对照（原版 vs 我们）

依据：逐组件读原版 `easytier-web/frontend/src/components/*.vue`（共 3716 行），
把每个组件用到的 i18n key 抽出来当作功能清单，再对照我们 `client/src/features`（2424 行）。

## 1. 缺口

| 能力 | 原版 | 我们 | 结论 |
|---|---|---|---|
| ACL 规则构建器 | AclPolicyTab.vue（494 行）：规则表、增删改、action、protocol、source/target、端口、默认动作、启用开关 | **已实现**：默认动作、规则卡片（名称/动作/启用/源/目标/协议端口）、上传下载顺序、删除、JSON 视图切换 | 已对齐 |
| 设备接入引导 | DeviceList/MainPage：enroll token、命令复制、说明 | **已实现**：从 console-info 拼出 `easytier-core --config-server <proto>://<host>:<port>/<user>`，一键复制 | 已对齐 |
| 设备列表增强 | DeviceList：搜索、别名、封禁、删除、实例数、位置、公网 IP | 已有别名/删除/封禁；**新增搜索、实例数列**；位置与公网 IP 缺（需要 geoip，我们只有接入地址） | 部分 |
| 设备详情 | DeviceDetails：central_networks、hostname、last_report、location、public_ip、status、version | 有页头 + 所属网络；缺 location/public_ip/last_report | 部分 |
| 网络详情增强 | NetworkDetail（1660 行）：临时成员、配置复制/重置、ACL 统计（bytes/packets/rule）、对端与路由 | **已做**：临时成员（签发凭据 → 现拼 `easytier-core -d --network-name <net> --network-secret <cred>` 并复制）、成员 override 的复制与清空；**缺**：ACL 统计（需要代理实例侧 RPC，控制面目前只代理 WebClientService）、对端/路由（设备详情页已有网络维度，未做实例维度） | 部分 |
| 网络创建 | NetworkList：networking_method（standalone/manual/gateway/public server）、secure mode、initial nodes | **已实现**：四种方法的select、Manual 显示节点地址、Gateway/PublicServer 显示公共服务器地址、安全模式复选框 | 已对齐 |
| 全局壳 | MainPage：语言切换、主题、文档链接、设备接入、封禁设备页、改密页、登出 | **已做**：主题、登出、文档入口（指向 easytier.cn）、封禁设备独立页 /devices/blocked、改密页 /account/password；**缺**：语言切换（copy 是单一 zh-CN 模块，被约 20 个组件直接 import，要做 i18n 需要先抽成 locale 表 + context） | 部分 |
| Dashboard | Dashboard：自动刷新、实例计数、在线成员、view all | 有设备/在线/最近在线 | 部分 |
| 登录 | Login：验证码、注册、SSO | 有注册（setup）、OIDC、无验证码 | 部分 |

## 2. 我们已有的（原版没有或更弱）

| 能力 | 说明 |
|---|---|
| 网络搜索/排序/计数 | 原版 NetworkList 有搜索，我们额外做了空态与计数联动 |
| 网络/设备独立子页面 | 原版是嵌套路由 + 侧栏，我们是独立路由，刷新可直达 |
| 高级设置面板 | MTU / 限速 / 监听地址 / 白名单 / 功能开关（原版只有 advanced_config 一个入口） |
| 设计令牌与对比度门禁 | `check-design-tokens` / `check-contrast` 强制 |
| 组件状态门禁 | `check-ui` 要求每个查询有 loading/error/empty |
| 真浏览器断言 + 截图 | Playwright 驱动 Chromium，产物入 `docs/screenshots/` |

## 3. 收口顺序

1. ACL 规则构建器（把 JSON 文本框升级为可视化规则表，仍保留 JSON 视图）
2. 设备接入引导 + 设备列表增强（搜索、实例数、位置、公网 IP、封禁页）
3. 网络详情增强（临时成员、配置复制/重置、ACL 统计）
4. 网络创建向导（方法选择、secure mode、初始节点）
5. 全局壳补齐（语言切换、文档入口、改密页、封禁设备页）
6. Dashboard 增强（自动刷新、实例计数、在线成员、view all）
7. 超越项：键盘可达性、空态一致性、e2e 断言固化为脚本
8. 收口：check-ui / 令牌 / 契约 全绿 + gate PASS + 截图更新
