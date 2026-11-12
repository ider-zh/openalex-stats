# 无重任务竞争复测与尺度检验

日期：2026-11-12，Asia/Shanghai。已等待audit PID858720退出、DuckDB进程消失，且web v6日志listening；开始前pidstat一秒观测web为0.00% CPU。普通后台服务仍存在，本报告不宣称专用裸机隔离。全部benchmark已经结束，未改动正式生产二进制或默认配置。

所有case固定GOMAXPROCS=46、真实all-ids.bin跨整个向量的32个不重叠窗口。每个build/lookup case为独立进程，使用GNU time记录CPU和峰RSS。

## 5M，三轮且轮转case顺序

时间02:51:19—02:52:11；以下为三次中位数。

| 实现 | build秒 | build加速 | 单goroutine lookup ns/op | 46并发aggregate ns/op |
|---|---:|---:|---:|---:|
| serial | 0.93717 | 1× | 245.8 | 7.214 |
| w16/s64 | 0.06651 | 14.09× | 227.5 | 7.471 |
| w32/s256 | 0.06855 | 13.67× | 233.0 | 7.557 |
| w48/s512 | 0.04487 | 20.89× | 257.6 | 7.698 |

并行build在每轮都明显快于serial。5M下w48更快且三次build在0.04414—0.04504秒；w32为0.04811—0.09501秒，存在较大波动。lookup没有同步获得相同比例收益，并发代价约3.6%—6.7%。

数据及全部原始资源记录位于[uncontended-5m-3x](uncontended-5m-3x/summary.json)。

## 50M，一次尺度检验

时间02:56:21—02:57:37。

| 实现 | build秒 | build加速 | 构建分配GiB | 构建峰RSS GiB | 单goroutine lookup ns/op | 46并发aggregate ns/op |
|---|---:|---:|---:|---:|---:|---:|
| serial | 14.37034 | 1× | 1.500 | 1.881 | 268.6 | 9.929 |
| w16/s64 | 1.14854 | 12.51× | 2.245 | 2.624 | 349.6 | 9.245 |
| w32/s256 | 0.48419 | 29.68× | 2.245 | 2.623 | 355.9 | 10.23 |
| w48/s512 | 0.61885 | 23.22× | 2.246 | 2.623 | 287.3 | 10.03 |

serial、w16/s64、w48/s512另用46个只读校验worker逐一验证全部50M ID的membership、原始ordinal及owned-ID，合计150M校验全部通过，耗时18.10秒。w32/s256使用相同通用算法，已在小规模及其他多shard回归中验证，50M该case本轮做了构建和查询基准，未单独再进行全ID校验。

构建进程整体平均CPU为100%、648%、970%、964%（100%=一个逻辑CPU）。这里包含未计时的输入采样/初始化，不能解释成build热区峰值，更不能声称所有阶段机器CPU均超过90%。B/op是分配量，RSS还包括400MB输入向量及运行时；并行多分配约800MB，符合50M×16B临时entry数组。所有lookup仍为0 B/op和0 allocs/op。

数据及完整证据位于[uncontended-50m-1x](uncontended-50m-1x/summary.json)。并发ns/op是总吞吐的倒数，不是单查询延迟。50M只运行一次，不能将相邻方案的小差距解释为统计显著。

## NUMA实际限制

服务器没有numactl：`command -v numactl`退出1，`numactl --interleave=all true`实际退出127并报告“未找到命令”。原始错误和退出码已保存。遵守要求，没有安装、改全局设置或绕过权限，因此没有NUMA interleave实测结果，也不声称默认放置最优。

后续补测（2026-11-12 04:00）：使用获允许的纯 Go 进程级 SYS_SET_MEMPOLICY wrapper 完成了同一50M真实ID、w32/s256的 default/interleave 各两轮构建与查询对照。实际 graph.test 的 numa_maps 和 GET_MEMPOLICY 均确认 interleave 跨节点0/1生效。原始结果与策略证据见 [NUMA补测报告](../numa-benchmark-20261112/README.md)。上段只描述早先 numactl 不可用的状态，NUMA候选现已完成实测；生产默认保持不变。

## 建议

建议v7先采用**32个索引构建worker、256个shard**，总GOMAXPROCS仍为46。理由是50M尺度build有明确数量级优势，而并发lookup相对serial约慢3%；构建的收益更可能超过全图查询的小幅代价。w48/s512为备选，单goroutine查询更接近serial，但本轮50M build略慢。w16/s64并发lookup最好，但构建慢于另外两个并行方案。

这一建议必须以v7的完整索引＋引用投影总耗时验收；5M最优项与50M最优项已经不同，不应把单次微基准外推为固定全量加速倍数。旧与新实现保持精确membership和ordinal语义，最终仍需比较同Q的统计结果artifact，而不能仅凭微基准正确性宣称全流程一致。

机器可读总报告：[followup-summary.json](followup-summary.json)。复现脚本位于上级research的`index-benchmark-uncontended-run.sh`与`index-benchmark-scale-run.sh`。
