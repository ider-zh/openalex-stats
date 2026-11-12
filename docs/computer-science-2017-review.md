# 中国 Computer Science 在 2017 年附近下降：核查报告

2026-11-12。使用本项目冻结的 2026-06-26 snapshot；只读核查，没有修改数据、黑名单、国家归属、学科规则或网站。

结论：低谷准确位置是 **2016 年**，2017 年已经回升。下跌发生在论文量；当年被引数同期持续上升。本次最直接的来源证据是无可用 DOI 的记录大幅减少，同时部分出版来源收缩。中文文献／CNKI 覆盖断档是与本地证据和既有独立研究一致的主要解释，不能据这条曲线推断中国计算机科研产出或实力真实下降。

## 1. 当前网站数据与独立重算

下表统一使用主数据 Core、国家全计数；Concepts Level0 score≥0.3，Topics 为 primary_topic 的 Field。引用是原范围内按施引发表年统计、归属到被引论文的当年被引，不是同期新论文累计引用。

| 年份 | Concepts 论文量 | Topics 论文量 | Concepts 当年被引 | Topics 当年被引 |
|---|---:|---:|---:|---:|
| 2014 | 152,324 | 58,703 | 749,144 | 338,571 |
| 2015 | 134,545 | 50,740 | 874,971 | 387,318 |
| 2016 | 122,569 | 43,374 | 1,061,169 | 450,544 |
| 2017 | 134,832 | 46,431 | 1,322,071 | 547,037 |
| 2018 | 161,538 | 55,780 | 1,757,894 | 716,856 |
| 2019 | 182,091 | 65,156 | 2,349,790 | 957,529 |

2014→2016：Concepts 论文减少 **19.53%**，Topics 减少 **26.11%**；当年被引分别增加 **41.65%**、**33.07%**。2016→2017 论文分别回升 **10.00%**、**7.05%**。

国家均分口径也有同样趋势：Concepts 论文 138,074.72→105,908.82，Topics 52,811.00→37,148.28（2014→2016）。因此不是仅由国际合作均分产生。

## 2. 直接的算术分解：有／无可用 DOI

对中国 2014、2016、2017 年两种 CS 分类的全部命中文献回查原 snapshot 的 DOI 和 primary source；每条核对原 ID、发表年、类型。按论文去重后再分别归入两种分类，不将两种分类相加。

| 分类 | 原始 DOI 分组 | 2014 | 2016 | 2017 | 2014→2016 净变化 |
|---|---|---:|---:|---:|---:|
| concepts | 无可用 DOI | 43,833 | 4,377 | 2,092 | -39,456 |
| concepts | 有可用 DOI | 108,491 | 118,192 | 132,740 | +9,701 |
| topics | 无可用 DOI | 17,039 | 1,569 | 742 | -15,470 |
| topics | 有可用 DOI | 41,664 | 41,805 | 45,689 | +141 |

仅看有可用 DOI 子集，concepts 的 2014→2016 变化为 **+8.94%**。这是来源敏感性对照，不是替代正式统计的新筛选标准，也不把 DOI 当质量金标准。

仅看有可用 DOI 子集，topics 的 2014→2016 变化为 **+0.34%**。这是来源敏感性对照，不是替代正式统计的新筛选标准，也不把 DOI 当质量金标准。

“无可用 DOI”在本核查中指未能从原始 DOI 字段提取 DOI 前缀；已确认非空前缀均符合 10.xxxx 格式。**无 DOI 不等于 CNKI**，本表不能直接给出 CNKI 对降幅的精确因果贡献。

## 3. 哪些来源在减少

以下是快照 primary source 名称分组的主要减少项。来源实体可能存在错误匹配或中文／英文版本混淆，因此保留原名；不把 host_organization_name 自动当成实际出版者的独立金标准。

| 分类 | 快照来源名 | 2014 | 2016 | 2017 | 2014→2016 |
|---|---|---:|---:|---:|---:|
| concepts | Applied Mechanics and Materials | 11,684 | 48 | 78 | -11,636 |
| concepts | Advanced materials research | 5,876 | 3 | 1 | -5,873 |
| concepts | WIT transactions on information and communication technologies | 2,727 | 0 | 0 | -2,727 |
| concepts | WIT transactions on engineering sciences | 929 | 0 | 1 | -929 |
| concepts | Advances in intelligent systems research/Advances in Intelligent Systems Research | 1,214 | 438 | 0 | -776 |
| concepts | The Scientific World JOURNAL | 737 | 3 | 1 | -734 |
| topics | Applied Mechanics and Materials | 4,238 | 6 | 16 | -4,232 |
| topics | Advanced materials research | 1,960 | 1 | 0 | -1,959 |
| topics | WIT transactions on information and communication technologies | 844 | 0 | 0 | -844 |
| topics | Advances in intelligent systems research/Advances in Intelligent Systems Research | 453 | 103 | 0 | -350 |
| topics | Computer Knowledge and Technology | 323 | 0 | 0 | -323 |
| topics | The Scientific World JOURNAL | 323 | 0 | 0 | -323 |

无 DOI 组中，与中文期刊覆盖有关的三个具体案例（Concepts）：

| 快照来源名 | 2014 | 2016 | 2017 |
|---|---:|---:|---:|
| Computer Knowledge and Technology | 561 | 0 | 0 |
| Computer Engineering and Applications Journal | 476 | 26 | 0 |
| Jisuanji fangzhen | 389 | 3 | 0 |

原始记录核验案例见 [source-examples.json](../research/cs-2017-audit/source-examples.json)。例如 W2360617161、W2352691509、W2375885166 的 primary landing URL 直接指向 en.cnki.com.cn 的 2014 年记录。另有 Applied Mechanics and Materials、Advanced materials research、WIT 系列的 DOI 来源减少。不能将所有减少都归为会议，也不能仅因 OpenAlex type=article 就认定为期刊论文。

## 4. 与独立研究的交叉印证

Zheng 等（2025，JASIST）专门研究了中国在 OpenAlex 中的异常走势，发现 2012—2016 年的下降主要与 CNKI 收录覆盖不连续有关；排除 CNKI 来源后，异常波动明显消失。[原论文](https://doi.org/10.1002/asi.70013)。其使用 2024-06-30 快照，研究范围与本项目不同；这里只用于机制交叉印证，不套用其比例作为本项目的贡献率。

Zhang 等（2026，Scientometrics）以中文期刊及万方作对照，也发现 OpenAlex 对中文文献的覆盖在 2000—2015 年较高、约 2010 年达到高点，其后较低，并记录了 DOI、署名及来源匹配问题。[原论文](https://doi.org/10.1007/s11192-026-05664-4)。这与本项目无 DOI 组减少、中文期刊来源断档的证据一致。

## 5. 已排查的其他解释

| 假设 | 检查结果 |
|---|---|
| Concepts 0.3 阈值导致 | 不是唯一原因。阈值 0 的中国计数由 223,173 降到 167,470；0.5 由 84,657 降到 75,714，均为 2014→2016。Topics 不用 Concepts 阈值，也下降。 |
| 主数据排除 XPAC 导致 | 不能解释主要降幅。Concepts 全数据 152,642→123,084；Topics 全数据 58,866→43,596，仍明显下降。 |
| 国家均分导致 | 全计数、均分都下降。 |
| ECI／小倪方法导致 | 论文量和被引数不经过 ECI／PCI 算法；方法切换不改变这些计数。 |
| 本项目从 2017 年改变黑名单 | 没有按 2017 年切换规则；黑名单／范围对所有年份一致。 |
| 引用数也同步下跌 | 未复现。当前当年被引数在所查窗口持续增长。引用包含历史论文库存，与当年新论文量不是同一集合。 |

## 6. 研究使用建议与边界

- 保留原曲线与原版数据，在解读中国约 2012—2016 年的下滑时标注来源覆盖不连续。
- 汇报同时给出全量与稳定来源／DOI 子集敏感性对照；在各国使用一致规则，不能只为让中国曲线平滑而删数据或插值。
- 国家信息补全解决的是已收录 Work 的国别缺失，无法补回根本没有收录的论文；这类断档需要另做中文来源的文献覆盖核查与匹配。
- 尚未取得完整 CNKI／万方逐年基准，也未对同源但国别未知的全部记录做反查。因此不能将所有缺口严格分配给文献未收录、国家归属缺失或真实出版量收缩；可以确认快照中的组成变化，不声称恢复了中国真实产出。

## 7. 工程与证据

先用网站 API 与 DuckDB 列裁剪独立重算，随后用 Go 并发回查原始 snapshot 的具体行。保存初始 Go、向量化基准与最终聚焦版本，未触碰原始数据或生产结果。最终范围是 2014／2016／2017，初步探索的 2010—2019 部分结果不混入最终分解。

最终 Go 40 workers 回查 **426,163 篇去重 Work、2,103 个原始分片**，所有原 ID／年份／类型核对通过。两个分类各三个年份共 6 项来源合计与独立计数逐项相等。另有 4 个分片的 Go／DuckDB 来源分组独立对照，以及 9 个原始记录的 CN 国家和落地页核对。

服务器证据目录：`/mnt/nvme0/public/openalex-stats/analysis-cs-2017-20261112/`。

本地：[汇总与闭合检查](../research/cs-2017-audit/summary.json)、[原始来源分组](../research/cs-2017-audit/venue-counts.json)、[原始行核验运行记录](../research/cs-2017-audit/venue-run.json)、[分层计数](../research/cs-2017-audit/cohorts.json)。

正式原 snapshot manifest SHA256：`10c1785b4a6f9757823fc6dd005a052716e2c510ea751659cab4bcfba4c35e2a`。原主数据 run：`4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455`。
