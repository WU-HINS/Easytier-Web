# 原版功能对照表

这份表来自**源码对照**，不是回忆：上游 `easytier-src/easytier-web/src/restful/` 的每个 `.route(...)` 与我们 `internal/httpapi/router.go` 的 `HandleFunc(...)` 逐条比对（去掉路径参数命名差异后取差集）。

## 1. 缺口（上游有、我们没有）：23 条 → 已补 18，剩 5

| # | 方法 | 路径 | 上游 handler | 作用 |
|---|---|---|---|---|
| 1 | GET | `/api/v1/auth/captcha` | `get_captcha` | 登录验证码（上游返回 SVG/空表示关闭） |
| 2 | GET | `/api/v1/auth/check_login_status` | `check_login_status` | 当前会话是否有效（前端轮询） |
| 3 | GET | `/api/v1/auth/logout` | `logout` | 登出（上游是 GET，我们只有 POST） |
| 4 | PUT | `/api/v1/auth/password` | `change_password` | 改密码（我们只有 POST /account/password） |
| 5 | GET | `/api/v1/sessions` | `handle_list_all_sessions` | 列出当前在线会话 |
| 6 | GET | `/api/internal/sessions` | `handle_list_all_sessions_internal` | internal 版本的会话列表 |
| 7 | DELETE | `/api/internal/users/{u}/sessions/{m}` | `handle_disconnect_session_internal` | 踢掉某台设备的会话 |
| 8 | GET | `/api/v1/blocked-devices` | `handle_list_blocked_devices` | 被封禁设备列表 |
| 9 | DELETE | `/api/v1/blocked-devices/{m}` | `handle_unblock_device` | 解封设备 |
| 10 | POST | `/api/v1/generate-config` | `handle_generate_config` | 把配置对象生成 TOML 文本 |
| 11 | POST | `/api/v1/parse-config` | `handle_parse_config` | 把 TOML 文本解析成配置对象 |
| 12 | GET | `/api/v1/networks/gateway-info` | `gateway_info` | 网关网络的信息（Gateway 方法专用） |
| 13 | POST | `/api/v1/machines/{m}/networks` | `handle_run_network_instance` | 在设备上启动一个实例（v3 设备视图） |
| 14 | GET | `/api/v1/machines/{m}/networks/config/{i}` | `handle_get_network_config` | 读取设备上某实例的配置 |
| 15 | GET | `/api/v1/machines/{m}/networks/info` | `handle_collect_network_info` | 收集设备上全部实例的运行时信息 |
| 16 | GET | `/api/v1/machines/{m}/networks/info/{i}` | `handle_collect_one_network_info` | 收集单个实例的运行时信息 |
| 17 | POST | `/api/v1/machines/{m}/networks/metas` | `handle_get_network_metas` | 实例元信息（名称/IP/版本等） |
| 18 | DELETE | `/api/v1/machines/{m}/networks/{i}` | `handle_remove_network_instance` | 停止设备上的实例 |
| 19 | POST | `/api/v1/machines/{m}/validate-config` | `handle_validate_config` | 在设备上校验配置是否可用 |
| 20 | POST | `/api/v1/machines/{m}/proxy-rpc` | `handle_proxy_rpc` | 把 RPC 请求代理到设备（控制台做跳板） |
| 21 | GET | `/api/internal/users/{u}/machines/{m}/networks/info` | `handle_collect_network_info_internal` | internal 版本的实例信息 |
| 22 | PUT | `/api/internal/users/{u}/machines/{m}/networks` | `handle_reconcile_managed_network_configs_internal` | internal 版本的配置对账 |
| 23 | DELETE | `/api/internal/users/{u}/machines/{m}/networks/{i}` | `handle_remove_network_instance_internal` | internal 版本的停止实例 |

归类：

| 簇 | 条目 | 说明 |
|---|---|---|
| A 会话与认证 | 1–7 | 验证码、登录状态、GET 登出、PUT 改密、会话列表与踢线 |
| B 设备封禁 | 8–9 | 上游路径是 `/api/v1/blocked-devices`，我们叫 `/api/v1/machines/blocked` |
| C 配置工具 | 10–11 | TOML ⇄ 配置对象的双向转换，前端"导入/导出配置"用 |
| D 网关信息 | 12 | Gateway 方法的网络信息 |
| E 设备视图（v3 兼容） | 13–22 | 上游按"设备 → 实例"组织：启动/停止/取配置/收信息/校验/代理 RPC |
| F internal | 7、21、22 | 上游给同进程前端用的 internal 变体 |

## 2. 我们多出来的（上游没有）：31 条

不是遗漏，是这轮重写的取舍，逐条都有归属：

| 类别 | 例子 | 理由 |
|---|---|---|
| 网络为中心的声明式 API | `POST /networks`、`/networks/{id}/members`、`/networks/{id}/advanced` | 意图 + 编译下发的模型，替代"逐设备写配置" |
| 初始化与自检 | `/api/v1/setup`、`/healthz` | 首次部署不需要手工塞管理员 |
| OIDC 绑定模式 | `/account/oidc`、`/auth/oidc/register` | 未绑定身份必须先注册用户名（不可变） |
| 管理员设置 | `/admin/settings`、`/admin/users` | 运行期可改的策略，不必改配置文件重启 |
| 设备通道 | `GET /@up/{username}`、`GET /ws` | 官方客户端 ws 的控制面入口 |
| 能力探测与 ACL | `/rpc-capabilities`、`/networks/{id}/acl-policy` | 客户端握手前的能力协商 |

## 3. 架构解耦现状

| 层 | 包 | 只依赖 | 备注 |
|---|---|---|---|
| 线格式 | `internal/wire` | 无 | TCP/WS/UDP 三种帧，含上游 datagram 会话协议 |
| RPC | `internal/rpc` | wire | mux、去重、服务注册表（按服务名回退匹配） |
| 会话 | `internal/session` | rpc、bal | 每设备一传输一条，`ByMachine` 选最优 |
| 意图/编译 | `internal/central` | bal | **纯函数** `Compile`，不碰 IO，可单测 |
| 存储 | `internal/db/{dl,dal,dsm,bal}` | SQL | 声明式 schema、双数据库 |
| HTTP | `internal/httpapi` | bal、central | 只做协议转换与鉴权 |

缺口里的 v3 设备视图（簇 E）正好是解耦的反例：它把"设备"当成一等公民，配置写在设备上。
我们要的是**兼容层**：把 v3 的请求翻译成我们"网络 + 成员 + 覆盖"的意图模型，
而不是把两套模型都塞进核心。

## 4. 收口顺序（建议）

1. 簇 A、B：纯 HTTP 层，改动小、风险低，先把"原版能做的事"补齐；
2. 簇 C：配置 TOML 互转，与 `central.Compile` 共用同一份 Overrides，避免第二套字段定义；
3. 簇 E：新增 `internal/legacyview` 适配层，只依赖 `Session.Call` 与 `Store`，不进入 central；
4. 簇 D、F：跟随 E 一起做，internal 变体直接复用同一 handler。


## 1b. 已补的 18 条（r216 六条 + r217 两条 + r218 五条 + r219 五条）

| 路由 | 做法 | 用例 |
|---|---|---|
| `GET /api/v1/auth/logout` | 转调已有的 POST 处理器 | TestOriginalConsoleAliasesAnswer |
| `GET /api/v1/auth/check_login_status` | 直接查会话，返回 logged_in / user_name / is_admin | 同上（匿名 false、登录后 true） |
| `GET /api/v1/auth/captcha` | 明确回 enabled=false（本控制台没有验证码） | 同上 |
| `PUT /api/v1/auth/password` | 转调改密处理器（字段名用 current_password） | 同上（匿名 401、登录后 204、新密码可登录） |
| `GET /api/v1/blocked-devices` | 转调 `/machines/blocked` | 契约检查已登记理由 |
| `DELETE /api/v1/blocked-devices/{id}` | 同上 | 同上 |
| `POST /api/v1/generate-config` | 新增 `internal/configtext`：配置 → TOML | TestConfigRoundTripsThroughToml |
| `POST /api/v1/parse-config` | 同上反向；错误按上游习惯放在 200 的 error 字段里 | 同上（含坏 TOML 仍返回 200 + error） |
| `GET /api/v1/machines/{m}/networks/info` | internal/legacyview → CollectNetworkInfo | TestAdapterUsesTheClientServiceMethods |
| `GET /api/v1/machines/{m}/networks/info/{i}` | 同上，带单个 inst_ids | 同上 |
| `POST /api/v1/machines/{m}/networks/metas` | → ListNetworkInstanceMeta | 同上 |
| `GET /api/v1/machines/{m}/networks/config/{i}` | → GetNetworkInstanceConfig | 同上 |
| `DELETE /api/v1/machines/{m}/networks/{i}` | → DeleteNetworkInstance | 同上（另含非法 UUID 不触发设备调用） |
| `POST /api/v1/machines/{m}/networks` | → RunNetworkInstance（配置必须带 instance_id） | TestAdapterStartsAndValidates |
| `POST /api/v1/machines/{m}/validate-config` | → ValidateConfig | 同上 |
| `GET /api/v1/sessions` | 会话快照（普通用户看自己的，管理员看全部） | TestSessionListingIsScopedAndAdminOnly |
| `GET /api/internal/sessions` | 同上，仅管理员 | 同上（普通用户 403） |
| `DELETE /api/internal/users/{u}/sessions/{m}` | DisconnectDevice | 路由生效，管理员专用 |

## 1c. 复核方式

差集是脚本算的：对比 `easytier-src/easytier-web/src/restful/*.rs` 的 `.route(`
 与 `internal/httpapi/router.go` 的 `HandleFunc(`，路径参数统一成 `:p` 后取差集。
