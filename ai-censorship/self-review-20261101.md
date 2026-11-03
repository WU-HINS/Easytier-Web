# 自审记录 2026-11-01

范围：控制面 Go 重写 + 前端重做 + 四层数据库 + 声明式对齐 + OIDC。

## P0（必须处理，未在本轮解决）

1. **UDP 传输缺失**：默认部署协议是 `udp://`，当前只实现 TCP/WS。上生产前必须补 UDP 可靠层（约 2.5k 行）或明确要求使用 `tcp://`。
2. **NetworkConfig 75 字段映射未生成**：编译器只填它决定的那部分字段，手工抄写会导致静默丢配置。应由 Rust 侧生成映射 + golden 用例。

## P1（应处理）

1. 声明式对齐只做「建表 / 加列 / 建索引」，**不比较索引定义**。改定义必须改索引名以强制重建（本轮 `uq_users_oidc` 改为 `uq_users_oidc_identity` 即因此）。
2. 对齐**不删多余列**，仅报告 `extra_column`。保守，但长期会积出僵尸列。
3. SQLite 备份是文件复制；PostgreSQL 需要 `EASYTIER_DB_BACKUP_CMD`，否则只有差异报告没有快照。
4. OIDC 只支持 RS256/384/512；未实现 ES256 与 `private_key_jwt`。
5. 前端未做 e2e（仅 `tsc -b` + `vite build` 通过），未接实时事件。

## P2（可延后）

1. 会话 Cookie 的 `Secure` 依赖 `--secure-cookies`，反代终止 TLS 时需人工确认。
2. `proxy-rpc` 的 JSON 字段名走 protojson（camelCase），与 2.7.0 前端的 snake_case 不一致。
3. 缺少限流与审计日志。

## 结论

本轮交付边界是「协议层 + 数据层 + 认证 + 设计系统骨架」，P0 两项在任何真实部署前必须解决。
