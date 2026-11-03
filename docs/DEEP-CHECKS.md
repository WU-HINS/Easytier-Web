# 深检记录

门禁每一轮都跑，但它跑的是"默认速度"的检查。**深检**（数据竞争、PostgreSQL 浸泡、
协议模糊、有状态序列长跑）耗时更长，按需执行——这份文件记录每一次深检的时间、命令与结论，
避免出现"我记得跑过"这种说法。

判定标准：深检不通过 = 当轮直接转为 P0/P1 处理，不允许"下轮再看"。


全量门禁（含引用证据扫描）的固定跑法：`EASYTIER_FULL_GATE=1 EASYTIER_TEST_PG_DSN=... ./scripts/gate.sh`——
22 项全绿，其中 `cited tests` 会重跑账本引用的全部用例。r139 实测通过。

引用证据深检必须先设好三个数据库变量，否则 PG 专属用例会被 `t.Skip` 掉——而"跳过"不等于"通过"，脚本会因此失败。

| 轮次 | 日期 | 深检 | 命令 | 结果 |
|---|---|---|---|---|
| r77 | 2026-11-01 | 数据竞争 | `EASYTIER_RACE=1 ./scripts/gate.sh` | 通过 |
| r115 | 2026-11-01 | 数据竞争（全包） | `go test -race -count=1 ./internal/...` | 通过：0 处 DATA RACE，10 个包全部 ok |
| r130 | 2026-11-01 | 引用证据全跑（含 PostgreSQL） | `EASYTIER_TEST_PG_DSN=... EASYTIER_HTTPAPI_PG=... EASYTIER_E2E_PG=... ./scripts/check-citations.sh` | 通过：173 个被引用的用例全部运行，0 跳过、0 失败 |
| r147 | 2026-11-01 | 新增 UDP 传输的并发与脏流量 | `go test -race -count=1 ./internal/wire/ ./internal/session/ ./internal/central/` 与 `TestUDPSurvivesGarbageAndManyPeers` | 通过：三个包 0 数据竞争；12 个并发对端夹带畸形数据报后仍全部 accept 成功 |
| r159 | 2026-11-01 | 设备抖动长跑（1000 轮，双方言） | `./scripts/soak.sh 1000`（SQLite 51.8s / PostgreSQL 39.6s） | 通过：两个方言各 1000 轮都不重推、无编译错误、无累积 |
| r188 | 2026-11-01 | 协议层模糊测试（各 120s） | `./scripts/fuzz.sh 120` | 通过：FuzzDecodePacket 与 FuzzFeedPacket 各跑满 120s，合计 245s；后者 699,227 次执行、新增 210 条有趣输入，无崩溃无导出用例 |

## 待补的深检

| 项 | 为什么还没做 |
|---|---|
| PostgreSQL 浸泡（长时间混合读写） | 需要单独占用数据库实例，安排在收敛阶段 |
| 协议模糊长跑 | 目前 `scripts/fuzz.sh` 是固定种子；长跑需要独立的机器时间预算 |
| 有状态序列 ×20 | 已在门禁内跑固定 20 轮，未做更长的随机序列 |

对应评审文件见 `ai-censorship/` 下同轮次的记录。
