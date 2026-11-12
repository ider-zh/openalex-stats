# 国家补全 v3：独立留出验证与全量旁路

2026-11-12。用户授权：并行做独立留出验证，通过的路线开展全量补全，保留原版，最终交覆盖率、准确率及统计变化 review。允许准备来源与索引，不允许未通过路线混入正式增强结果；本次不替换原网站。

## 冻结与独立性

原快照 2026-06-26，manifest SHA `10c1785b4a6f9757823fc6dd005a052716e2c510ea751659cab4bcfba4c35e2a`。Core/all 原统计及引用图不改。

由独立验证 agent 管理新随机样本及 gold。排除 6,559 个旧 pilot ID 和其他已曝光的开发／QA 记录，共 7,035 个排除 ID，其中 6,766 位于原 UNKNOWN 抽样框。新的 UNKNOWN 抽样框为 277,411,702；按原 Core／XPAC、年代、类型设计层，每层最多 256 篇，最终 12,846 篇。两个旧 census 小层已耗尽，保留为 N=n=0，不补抽替代。

概率样本 SHA `ed8f0afae950f434315426f38b46941ae64ac7dd6c416706bcd6fc21e3504a69`。独立验证协议 SHA `e3d8e4a8b1aae3b5bff8f3d684ac65bc3b325a25ed55546dc6ae08f07ccff6b2`。这些样本用于恢复率估计，不能拿候选富集精度样本代替总体覆盖估计。

各路线在首次读取新 holdout 输入前冻结政策、来源、代码及 binary SHA。候选数不足时，允许从另一个预声明的、与开发集不重叠的候选框按固定 hash 选择验证；抽样规则先冻结。失败不能删除、替换或补抽，以同一 holdout 调参后不得再宣称独立验证。

## 通过标准

- 每条自动准入路线至少 128 个独立候选，按预声明 hash 样本核验，unknown/unverified 留在分母。
- 联合“同一论文身份＋作者署名国家证据”点准确率 ≥99%，双侧 95% Wilson 下界 ≥97%。
- 存在未隔离的系统性身份错配、出版机构总部当署名国、参考文献串取等错误时，不通过。
- 可见作者署名国家集合完整性另报，目标 ≥95%；来源缺少全部作者信息时，不能以来源内一致冒充整篇真实完整性。
- 独立 agent 对原 XML／PDF／原署名证据核验，不用抽取器自身 Countries 输出作为 gold；仍不声称人类作者本人确认或用户已经 review。
- 不足、失败或适用范围受限的路线独立报告，不阻塞已通过路线；不得为了满足全量承诺无限抓取公开全文。

## 角色与来源范围

| 分支 | 责任 |
|---|---|
| 独立验证 | 新概率样本、排除与权重、候选精度样本、来源独立核验、门禁及精度区间 |
| 元数据 | Crossref 作者署名 dump；完整 January-2026 PubMed baseline（daily updates 未覆盖单列）；原 Work 标识符窄索引、冻结自动准入、通过后全量 patch |
| 全文 | 官方 PMC bulk map／inventory、JATS 原文；PDF 独立门禁／成本；通过路线全量旁路 |
| Root | 原机构／文本规则验证、共享 UNKNOWN 位置框、版本与冲突合并、统计重算与差异报告 |

Crossref 不仅凭 DOI 自动补国，个人署名＋有效 ORCID 等身份门槛需满足；机构作者、机器 ROR、总部位置单列 review。PubMed 只取本文作者 affiliation，严格区分期刊国与参考文献 DOI，保留 baseline vintage，不把未扫当不存在。不调用逐 DOI／PMID 元数据 API。

## 原规则复用

v1、v1b 仍是未应用的历史候选，不能未经验证自动并入。Root 先验证 v1 中原国家为空、无冲突、无机构推断新增且有明确国家尾段的纯文本通道；规则沿用原冻结版本，不在留出集上调词典。机构-only、已有国家扩展、v1b 地理／机构别名通道单列，未通过者不并入本次主增强结果。

## 共享框与工程约束

已用 Go 32 workers 在 16.26 秒导出原 UNKNOWN 全量位置索引：277,418,468 条，Core 150,377,414、XPAC 127,041,054，2,446 个 Parquet 分片。

229：`/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/shared/unknown/manifest.json`。含 `id/ordinal/year/type/is_xpac/source_index/raw_row_index`，manifest 映射原始路径与 cache 路径。此框保留全部原 UNKNOWN，供全量补全；独立验证另排除开发集。

全量 raw DOI／PMID／title 使用成熟列裁剪、按分片 semi-join，避免对 2.77 亿条逐条 Seek。Go 用于本地并发、索引、来源解析、规则、合并和统计；成熟 DuckDB／GROBID 用于明确更有效的投影与 PDF 版面解析。保留 checkpoint、失败、代码与源文件 SHA。755 GiB RAM 至少保留 64 GiB available，不停止其他任务，不修改全局代理／Java／Docker 配置。

## 统计与交付

主增强版本只将验证通过的国家加入原 UNKNOWN Work；原已有国家不删除或覆盖，已有国家扩展另列。按 Work／国家去重并解决跨来源冲突；原始版本、纯规则版本和外部增强版本可追溯。国家完整性未知需显式保留。

Q、年份、类型与引用边保持相同；重算国家／年／四个分类层级的论文量、当年被引、份额、RCA、新谱 ECI／PCI 和小倪反射 ECI／PCI。比较 2000—2025 完整年和 2000—2026 部分年，保留 2000 年以前被引目标。报告覆盖率、全量新增数、独立验证精度与区间、失败／适用范围、统计与排名变化及成本。最后交用户 review，原网站继续使用原版。

## 执行期间数值修复记录（不改变来源准入政策）

2026-11-12，文本增强 Core 的 20/21 轮反射敏感性审计在 2026 Topics/Subfield 论文量 PCI 检出标准化均值约 3.15e-6，超过既定 1e-6 门槛。接近常数的原始迭代值先直接计算大均值再相减，造成残差中心化误差；失败目录和证据保留。

生产反射标准化改用观测值平移、补偿求和及两遍总体方差，独立审计采用不同的平移 Welford 算法。两者数学定义仍是总体 z-score；不改 16/17 主迭代、12/13 与 20/21 敏感性轮次、32 ULP 信号要求、1e-14 标准差底线或 1e-6 审计门槛，谱方法不改。Windows、Linux 单测及独立静态复核通过。使用同一修复后二进制为原数据另建 Java 基线，再与增强版比较，单独报告数值修复影响，原基线文件保持不变。

## 追加：同一论文多 Work 的验证单位（新候选 gold 之前冻结）

独立验证方 `validation/protocol-r3.json` SHA `3413756a7e94e2d95fda7a54348061227320ed86ec1844b19c3e0d5c5546c6a2`。各路线整体及净新增候选框分别按已知规范 DOI／PMID／PMCID 构建论文键连通分量。以分量最小 WorkID 的既定 route hash 选择至少 128 个独立 cluster；选中 cluster 的全部候选 Work 核验，任一身份／国家错误使整个 cluster 失败。cluster 准确率／Wilson 下界仍使用既定 99%／97% 门槛，所有成员按 Work 计算的点准确率也须 ≥99%。

不能以 first-wins 任意丢掉共享论文键的 Work，不能按 gold 结果删样本；源框内原 3,000／净 10,000 的样本不替换。源框外关联成员作为明确关联证据任务，不伪称新增独立随机样本。缺键只能以 Work 为独立单位代理，报告隐蔽重复限制。新 cluster 分量规则不回溯调旧文本 256 的结果。

## 追加：恢复优先级与外部运输

净新增域的固定优先级：文本 → 已通过 Crossref → 已通过 PubMed → PMC。`metadata/recovery-priority-v1.json` SHA `77f4c06dc1af52940792d3c1d5f4c2af7c28d8aa7c449a92c5be3a338d39c2ff`。各路线完整候选精度框可以并行验证；净域框冻结时只扣除前序已通过双门禁的最终全量 patch，并绑定 patch 与 gate SHA。失败或不足路线不占恢复排除集。

PMC 官方在 2026-08 移除了旧批量打包文件；现使用官方 bulk identifier map／每日 S3 inventory 离线匹配，再取有界文章版本的静态对象，未调用逐篇身份转换／元数据 API。依据：[PMC AWS 文档](https://pmc.ncbi.nlm.nih.gov/tools/pmcaws/)、[官方迁移公告](https://ncbiinsights.ncbi.nlm.nih.gov/2026/02/12/pmc-article-dataset-distribution-services/)。对象版本、清单 ETag、MD5、失败和 checkpoint 均保留，不以最高版本号自动当作正式发表版本。

静态 S3 并发 3 是先前自设保守运输参数，并非官方 S3 限流。保持源对象、样本、身份与国家规则、MD5／超时／重试不变，实测调整 3→8→16 并记录成本；净 cohort 可继续短测 32／64，出现限流或错误上升即回退。该调整仅影响运输吞吐，不允许更换失败样本、扩抽或调国家规则。逐篇网页全文抓取不作为无限回退路线。

## 追加：PubMed 集体署名域外角色隔离（首次该路线 gold 之前）

静态审查发现来源 adapter 未传 CollectiveName 标记，使整篇身份由个人作者满足后，冻结引擎仍可能加入仅集体署名支持的国家。原始 policy 的个人署名范围不扩展，冻结引擎及原始 outcomes 保留。

`metadata/personal-country-scope-v1.json` SHA `0927fdbaf719800370451b896b4bf9555e120a41c59fc91c169a1d827a1e79fd`；角色差分代码 SHA `38fb4e3aa0ca2abac0db3d1ae327dbc6013551f087a865375fb8218c91f7dbea`。若任一提议国家在所有已准入身份记录内只有 CollectiveName 支持、无个人署名或原已明确允许的 legacy ArticleAff 支持，隔离整个 Work；不静默删国家后伪称旧规则输出。个人／集体同国或跨记录已有个人支持不误删，legacy 保持 limited 标记、不伪装个人。

仅清不同角色的来源 affiliation 做冻结引擎视图差分，所有原作者名字／ORCID 保留；逐记录验证角色并集等于原提议集合，合成 QA 先冻结。整体及净域精度框均在此范围固定后抽，不能根据后续 gold 调整；不回溯改变已冻结 PMC 策略或失败分母。

## 最终执行记录：2026-11-13

外部 Crossref、PubMed、PMC／PDF 未通过既定推广条件，实际准入 0。严格文本路线原独立留出仍为 255／256；后续地点与邮政地址冲突审查保守延后 142 个完整 Work，不替换国家，不删除旧验证失败，不声称延后后的条件精度另获独立验证。

最终 r3 补丁 2,667,126 篇，SHA-256 `fe6d3abaf6d94e5ca3c209c6a5e5270f7c8c85964dc34ac3d3ce9e6c662db5f3`。Core／All 全量旁路统计已完成，各 25 项独立检查、共 38 件产物大小及 SHA 核验通过；原版和生产数据保留。完整结果、范围限制和准确率解释见[最终 review](country-enrichment-v3-review.md)，公开汇总、补丁与复算输入见[归档包](../results/research-review-20261113/README.md)。这是执行结果记录，不追改冻结门槛。
