# 229真实ID索引初步基准

时间：2026-11-12 02:41:30—02:41:57（Asia/Shanghai）。状态：**完成，但有其他重负载竞争，仅count=1，不据此切换生产默认实现。**

独立目录：`/mnt/nvme0/public/openalex-stats/index-benchmark-20261112-5561618b61`。只复制graph、model及go.mod/go.sum并编译独立测试二进制，未修改正式v4、数据或其他任务。

输入来自正式`core-goal-v1/all-ids.bin`，共510,372,821个ID；从整个向量均匀分布的32个不重叠窗口取5,000,000个真实ID，未读取全部4.08GB。GOMAXPROCS=46，Go1.26.0，E5-2678 v3双路服务器。

## 结果

| 实现 | 构建秒 | 相对serial | 构建分配MB（十进制） | 构建进程峰RSS MiB | 单goroutine查询ns/op | 46并发查询aggregate ns/op |
|---|---:|---:|---:|---:|---:|---:|
| serial | 1.46151 | 1.00× | 191.32 | 226.21 | 731.4 | 17.31 |
| parallel w16/s64 | 0.28186 | 5.19× | 271.40 | 302.33 | 522.0 | 17.19 |
| parallel w48/s512 | 0.27987 | 5.22× | 271.80 | 298.59 | 479.0 | 15.57 |

构建吞吐约为3.42M、17.74M、17.87M IDs/s。单goroutine查询吞吐约1.37M、1.92M、2.09M次/s；46并发总吞吐约57.77M、58.17M、64.23M次/s。查询包括相同的伪随机ID生成与12.5%确认缺失查询，所有lookup均为0 B/op、0 allocs/op。

并发ns/op是总操作数除以wall time的倒数，不能解读为单次请求延迟。构建B/op是分配量，非最终常驻内存；`time -v`峰RSS还含输入样本、运行时与未计时初始化。并行实现多分配约80MB，与5M×16B临时entry分桶数组一致。内存规划还需额外计算原ID向量和其他阶段活数据。

## 正确性

独立`TestParallelIndexRealSample`逐一验证5,000,000个ID的membership、原始ordinal和owned-ID值，serial、w16/s64、w48/s512全部通过；无效ID0均miss。小规模单元测试另覆盖zero、duplicate、取消、不同shard数和Graph.Count等价。

## 竞争与局限

在w16 lookup期间观察到audit的DuckDB PID859059约4033% CPU、audit PID858720约347%、web PID855541约87%。该观察已实时通知root。`ps %CPU`为进程生存期平均指标，非区间CPU采样，但可明确确认有重任务重叠；结束后的进程快照也保存在`competition-observed.txt`。

因此，这一轮只能说明并行构建有积极信号及正确性通过，不能精确推断无竞争加速比，也不能证明48workers优于16workers。root已确认待大审计完成后再安排无竞争复测；本轮后未继续更大benchmark。

## 证据

- `summary.json`：机器可读指标、输入artifact身份和限制。
- `*-build.txt`、`*-lookup.txt`：Go原始benchmark输出。
- `*.resources.txt`：每个独立进程的GNU time资源数据。
- `correctness.txt`：真实5M ID逐一验证结果。
- `source-dimensions-checkpoint.json`：输入ID artifact的已验证生产checkpoint及SHA256；未重新全读4GB只为重复hash。
- `source-files.sha256`：独立源码包与关键Go代码摘要。
- `../index-benchmark-run.sh`：顺序执行脚本。
