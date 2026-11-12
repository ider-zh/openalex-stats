# 交付与复现

正式展示：[OpenAlex Atlas](http://192.168.1.229:8899)。

在页面切换Topics/Concepts与对应层级，固定学科，选择多个国家，再切换论文量、当年被引、份额、RCA、国家ECI或学科PCI。复杂度可选择论文量或当年被引作为输入。默认2000—2025；2026明确标为不完整年。

## 正式数据

- 服务器：ssh ider@192.168.1.229。
- 数据目录：/mnt/nvme0/public/openalex-stats/results/core-goal-v2。
- run_id：e1c0135626bc7dfe950894a9e666df5d181d8c0d673023655221f27921445844。
- 快照2026-06-26，完整覆盖2,446文件、510,372,821条Works。
- 主集合269,194,661篇；输出年份内201,267,475篇；限定集合内2,363,318,327条年度引用边。
- Topics Field/Subfield、Concepts Level0/1共四层；论文/被引两个输入，形成八套年度ECI/PCI，27年共216个模型。
- 六个统计场景：国家均分/全计数×Concepts阈值0/.3/.5；主复杂度使用均分/.3。

完整产物及SHA256以数据目录completion.json为准；镜像在research/formal-completion-v7.json。页面可下载当前曲线CSV或全部Parquet/JSON文件。

| 文件 | 内容 |
|---|---|
| statistics.parquet | 六场景国家—年—学科计数、份额、RCA |
| cube.parquet | 主场景基础计数 |
| country_eci.parquet /discipline_pci.parquet | 全部国家/学科复杂度、有效性状态 |
| rca.parquet | 两种输入的RCA长表 |
| eci.json /model_diagnostics.json | 完整模型与诊断 |
| dictionary.json /scenarios.json | 标签与权重/阈值场景 |
| coverage.json /methodology.json /run.json | 过滤质量、实际口径、运行与性能 |
| completion.json | 原子完成清单及输出校验 |

## 程序与版本

远端程序目录：/home/ider/tmpdata/openalex-stats-dev-20261112。

- 正式统计由保留的oa-stats-v7执行；源归档source-v7.tar.gz，精确身份见research/execution-v7.json。
- 当前交付/服务程序oa-stats-v8，仅进一步并行化输入校验及调整reader默认值，不改变统计内核。源差异逐文件证据：research/release-v8-code-diff.json。
- 服务PID877825，日志web-formal-v8.log；实际程序SHA与当前数据身份由research/browser-formal-v8/service-identity.json记录。
- 最新源码/证据归档、二进制SHA、源快照与验证路径由research/release-v8.json统一索引。
- 不覆盖原始快照、旧正式结果或其完成清单；改变build/统计口径应使用新的不可变输出目录。

[README](../README.md)给出构建、完整计算、准确同版本恢复、独立审计、场景配置、服务与SSH转发命令。当前服务使用nohup运行，SSH退出后仍可访问；服务器重启后按README重新启动服务。

## 验证与性能结论

- 原正式全量独立SQL重建：2,141项检查通过。
- 优化版全部2,562,778行主表、14,966,202行统计表、29,932,404行RCA与已审计基线逐项对照通过；浮点容差abs1e-7+rel1e-10，实际舍入差异保留。
- 整数层2,446引用分片与64计数分区的已提交摘要全部一致；没有把摘要比较说成重读大二进制。
- 全216模型独立非对称算子、跨年符号及两个分数长表核对通过；原始→缓存402,439行、23项字段/质量检查无差异。
- 正式同一v7程序/配置复跑保持run_id、完成时间及完成清单SHA不变，无重复累计。
- 当前代码测试/vet、229的race检查通过；最终正式浏览器24项、桌面/移动截图与API/表格/实际绘制数值一致。
- 全量索引406.17→9.87秒；代表性引用阶段115.28秒，CPU均值92.01%。连续60秒窗口均值93.27%，保守扣除整段GC后93.09%；并非每个单秒都超过90%。
- 正式v7运行内部计时576.19秒；进程启动至完成清单提交约579.80秒（9分40秒），复用了规范缓存和维度，不包括原快照下载及前期缓存构建。
- 最新全量校验94.35→5.08秒（24workers，热缓存、每模式一轮）；不将它说成v8五秒完成全部统计。
- 运行采样峰值RSS134.53GiB；全程整机CPU均值29.47%，其他阶段未普遍达到90%。没有通过无用分配追求90%内存。

[方法与质量](methodology-and-quality.md)、[性能完整报告](performance-report.md)、[独立逐项验收](independent-completion-audit.md)列出证据和限制。51.31%的输出年份论文缺少国家，保留在UNKNOWN中；国家比较反映可识别署名信息，不代表全部国家科研产出的完整普查。ECI/PCI是逐年相对结构指标。

