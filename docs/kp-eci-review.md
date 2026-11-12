# 朝鲜国家 ECI 排名：数据、模型与方法联合审查

审查日期：2026-11-12。研究对象是 2026-06-26 冻结快照、原始国家标签、国家均分、Concepts 同层命中全计数且 score ≥ 0.3。本文为旁路研究，不修改正式数据或网站。已完成全部 KP 标记合格文献的定向原始记录核查；冲突规则候选仍待人工 review，不能将其数量当成已确认错误数量。

## 当前可以确认什么

朝鲜靠前的排名是真实返回的模型结果，已经排除 KP/KR 显示混淆和前端排序错误。同时，确实发现了上游 OpenAlex 机构误匹配，不能把当前输入网络解释成可靠的朝鲜真实科研组合。Concepts Level 0 另外存在方向锚点很弱的问题；这些机制必须分开处理。

| 2025 Core、引用量输入 | KP 谱排名 | KP Java 排名 | 优势学科数 |
|---|---:|---:|---:|
| Topics Field | 9 / 229 | 9 / 229 | 12 / 26 |
| Topics Subfield | 7 / 229 | 6 / 229 | 66 / 252 |
| Concepts Level 0 | 2 / 229 | 228 / 229 | 5 / 19 |
| Concepts Level 1 | 1 / 229 | 1 / 229 | 73 / 284 |

229 是该年度模型中的有效国家数；248 是可查询国家／地区字典数。筛选展示国家不会重排全球名次。引用计时使用施引文献发表年，国家、学科归属使用被引文献。

## 已确认的机构误匹配

快照把部分不同国家的泛化“自然科学研究所／学院”署名，额外匹配到 `I4210158445`，即标记为 KP 的 Institute of Natural Science。完整出版者署名独立确认了下面两例。

| 作品 | 快照国家集合 | 出版署名支持 | 发现 |
|---|---|---|---|
| [Decision tree methods: applications for classification and prediction](https://pmc.ncbi.nlm.nih.gov/articles/PMC4466856/)，W861925310 | CN、KP | CN、US | 出版署名为上海交大、斯坦福及美国 VA 体系，不支持 KP；快照还漏了 US |
| [MXene: an emerging two-dimensional material for future energy conversion and storage applications](https://pubs.rsc.org/en/content/articlehtml/2017/ta/c7ta09094c)，W2768088004 | KR、KP | KR | 自然科学研究机构属于首尔高丽大学；出版者三个署名均为韩国，不支持 KP |

这是作者单位匹配错误，不能通过交换 KP/KR 代码解决。原解析器对抽查样例得到的国家并集与快照完全一致：错误已存在于上游匹配结果。

同快照的[机构实体记录](../research/kp-eci-audit/data/institution-record.json)确实将 `I4210158445` 定位为平壤／KP。需要修的是具体署名与机构之间的错误链接，不能把这个真实朝鲜机构的全局国家代码改为韩国。

证据：[出版署名核验及访问范围](../research/kp-eci-audit/methodology/publisher-crosscheck.json)。第三例只有部分出版信息，未作为另一例完整确认。报告不复制署名中的邮箱或整篇论文。

## 两例最小纠错诊断的实际影响

分别试验了“仅删除出版署名不支持的 KP，保留其他现有国家”与“按两篇完整出版署名恢复国家集合”。前者对决策树论文留下 CN，但不声称国家集合已经完整；后者改为 CN、US。两版均对所有相关国家重新均分，更新全部 RCA、优势网络及两种模型，不只减 KP 分子。

2025 Core 的 KP Topics 引用赋值由 3,122.2923 降至 2,972.7923，减少 **149.5，约 4.79%**。两版本在四层、两方法的 KP 排名均未改变。Concepts 0 的整个二值优势网络不变；Concepts 1 的 KP 优势度反而由 73 增为 79，因为分母与学科构成改变后又有若干 RCA 跨越 1。

因此，两例足以证明国家污染，但不足以证明全部高排名由它们造成。纠错对排名没有单调性，不能将该试验称作“真实排名下界”。

证据：[仅删 KP 的诊断](../research/kp-eci-audit/model/publisher-patch-remove-kp-only-impact.json)、[恢复出版国家集合的诊断](../research/kp-eci-audit/model/publisher-patch-publisher-country-sets-impact.json)。更广的原始地址冲突核查与这些逐篇外部确认案例分开。

## 全量定向核查与保守冲突候选

全部合格 KP 标记文献为 Core 4,242 篇、Core+XPAC 4,433 篇，定位到 1,127 个原始分片。逐篇核对 ID、国家并集、年份及 XPAC 身份一致。另用 Go 按原始物理行独立补查两处原文和全部作者的结构化机构：10,595 个 KP 作者条目中，两处原文字段合并前后无新增字符串；保护检查仍实际使用两处字段及全作品所有作者，不依赖抽样推断。

保守规则要求：唯一带 KP 的匹配机构是上述泛化机构；全部相关作者的每条非空原文都以明确外国地址段结束；全作品没有 KP 地名、国家别名／缩写正证，没有其他结构化 KP 机构，也没有未能判明的额外机构 ID。Unicode 转写及正反例另行测试。它筛出的仍是**地址／机构冲突候选**，不是逐篇出版者确认的错误。

- Core 候选 **590 篇**，全数据候选 **616 篇**；其中 20 / 21 篇在删除不支持的 KP 后会成为 UNKNOWN。
- 2025 Core 候选涉及 **285 篇被引目标、2,556 条引用边、944.4254 个 KP 均分引用赋值**，占该年 KP Topics 输入约 **30.25%**。
- 另有 985 篇 Core 文献只有部分外国地址／城市证据，不进入这一保守诊断。
- 泛化机构本身关联 2,672 篇 Core 文献、约 66.43% 的 KP 2025 引用赋值；**这不是 66.43% 全部错误的认定**，不能全机构拉黑。

最终候选补丁：[strict-generic-foreign-patch-v3.json](../research/kp-eci-audit/data/strict-generic-foreign-patch-v3.json)，SHA-256 `de405b5628a558fac25c2eeef28b591fe7e27ef21d6147433c53d3e0650fa12c`。原始来源和保护规则门禁：[candidate-source-gate.json](../research/kp-eci-audit/data/candidate-source-gate.json)。

完整数据检查、抽查边界与复现方式见[数据报告](../research/kp-eci-audit/data/report.md)和[机器验收入口](../research/kp-eci-audit/data/acceptance.json)。

对该候选作“仅移除 KP、保留原其他国家或回到 UNKNOWN”的诊断，重新分配所有国家的权重、重算完整 RCA／M 和两种方法；每种方法的正负方向与其原模型对齐，以隔离 2025 输入变动影响。这不是从 2000 年重新建立时间方向链的新正式版本。

| 2025 Core 层级 | 原谱排名 → 候选诊断 | 原 Java 排名 → 候选诊断 |
|---|---:|---:|
| Topics Field | 9 → 14 | 9 → 15 |
| Topics Subfield | 7 → 7 | 6 → 6 |
| Concepts Level 0 | 2 → 5 | 228 → 225 |
| Concepts Level 1 | 1 → 1 | 1 → 1 |

KP Topics 输入由 3,122.2923 降至 2,177.8668，但 Concepts 1 仍居首，优势度反而从 73 增至 83。因此，上游污染已经证实且有可量化影响，**目前证据仍不能将全部高排名归因于这些候选**；候选也没有恢复完整真实国别。剩余输入质量、选择性可观测组合与指标有效性须分别审查，不能为获得预期排名继续挑选剔除规则。

例如 Pure mathematics 的 KP 引用赋值仍为 9.0833，RCA 却从约 0.9415 变为 1.4058；并非引用增加，而是国家总分母下降后新出现优势边。完整重分、世界边际和 248 国排名变化见[绑定最终 v3 补丁的诊断](../research/kp-eci-audit/model/strict-generic-foreign-patch-v3-impact.json)及[三版本影响汇总](../research/kp-eci-audit/model/patch-impact-summary.json)。

## 没有发现哪些计算错误

从不可变 cube 重新构造输入，对 2020、2023、2025 年、两语料、四层、两输入独立重构 48 个谱模型及 48 个有限反射模型。优势边差异为 0；允许整体正负号后，分值最大差为 0。底层矩阵与对称算子独立构造，特征分解使用相同的 Go Gonum 库。8 份原输入文件 SHA 均匹配 completion。

另有 12 场景端到端检查：全国家 API 的排名等于 `1 + 分值更高的有效国家数`，Bump、悬浮表、KP 中文名称及 KP/KR 不同国旗均正确，单独选择 KP 不改变其全球排名。

证据：[完整模型审计](../research/kp-eci-audit/model/report.md)、[展示链路](../research/kp-eci-audit/display-audit.json)。数学重算通过不等于输入国家标签真实。

## Concepts 0 方向问题与其他层的区别

Core Concepts 0 引用模型的初年谱分值与优势多样性的相关仅 +0.00812，方向锚点很弱。跨年保持同一方向后，2025 与多样性相关为 −0.10981；Java 则为 +0.11173。两种方法全国家分值相关约 −0.99994，造成 KP 第 2 与第 228 的差异。

机械改为每年与多样性正相关，会让某些相邻年份几乎整个世界排名反转，不能当作无声修复。Concepts 1 的两种方法与多样性相关均约 +0.50，而且 KP 都排第 1，故不能用符号问题解释该层异常。

证据：[完整方向链](../research/kp-eci-audit/model/temporal-orientation.json)。

## 为什么简单加样本门槛不够

对全部国家统一试验边支持阈值 `X_cd ≥ 0、1、5、10、25、50`，固定原 RCA 世界基准、剔除不满足支持量的优势边，重算模型并按各自原方向对齐。2025 Core Concepts 1 的 KP 在前五档两法均第 1，最后一档第 2；Topics Subfield 在阈值 25 时反而升到两法第 1。这些是工程敏感性网格，不是文献证明的通用门槛。

2025 Core 的 KP 标签涉及 1,467 篇被引作品、7,699 条保留引用边；Top1／Top10 加权引用占比约 3.65%／12.34%，并非一篇论文独占。部分学科单元仍很稀疏，例如 Computer engineering 的 8 个均分引用赋值来自一篇被引作品。应同时报告国家总支持量、单元支持量及来源质量，不能仅看某一个合计。

当前约 83.63% 加权引用来自带多个国家标签的作品。这只能称“多国标签比例”：已知错误机构匹配也会制造该现象，不能直接当作真实国际合作比例。

证据：[集中度](../research/kp-eci-audit/data/citation-concentration.csv)、[学科支持量](../research/kp-eci-audit/data/subject-citation-contributions.csv)、[全国家阈值敏感性](../research/kp-eci-audit/model/edge-sensitivity.csv)。

## 对指标解释和下一步的建议

ECI 是归一化学科组合的网络结构指标，不是论文总量、引用总量或国家综合科研实力的排序。组合在谱轴的哪个位置、国家标签是否正确以及观察到的文献是否具有代表性，是不同的问题。[原始方法与解释文献审查](../research/kp-eci-audit/methodology/review.md)

优先将“明确地址／出版署名与匹配机构所在国冲突”做成适用于所有国家的质量检查；以独立候选补丁保留依据，审查后再重算国家均分、份额、RCA 和两种复杂度。不能只针对朝鲜删机构，也不能用使其排名下降的阈值来选择方法。覆盖率补全与已有标签纠错应分别报告。

当前正式基线、方法和网页均未改变。覆盖率下一轮可考虑的外部元数据、全文署名和机构消歧路线见[国家覆盖率后续方案](country-enrichment-next-steps.md)；增加标签与纠正原标签应同步评估，不能把原有“已知国家”直接当作人工真值。
