# OpenAlex 文献范围调研来源与筛选建议

调研日期：2026-11-12。目标：为国家、年份、学科的论文量、年度被引和 ECI 提供可审阅的文献范围。以下为建议，尚未成为正式统计规则。

## 最重要的版本差异

本项目快照日期为 2026-06-26。`work-types` 字典列出 19 类，但全量 Works 审计发现 `works.type` 实际有 24 类：字典外还存在 `book-section`、`database`、`grant`、`report-component`、`software`。字典并未覆盖全部实际取值，筛选必须以 Works 实际数据为准。[本地全量审计](snapshot-type-audit.json)

OpenAlex 现行 25 类规范在 2026-06-29/30 确定，类型重分类在 2026-07-15 公布，均晚于快照。全量审计中未出现现行 `conference-paper`、`conference-abstract`、`data-paper`、`software-paper`、`book-review`，不能把这些类别当成快照已经具有的字段值；也不能将上述五个字典外旧取值误称为这次规范新增类型。

官方说明，旧类型主要继承上游元数据，`article` 曾是缺少可靠类型时的默认归类；新分类首轮改变约 4,960 万条记录。这证明旧 `article` 和 `review` 适合作为候选集合，不能作为“已精确识别的研究论文及综述”的保证。[OpenAlex 类型重分类公告](https://blog.openalex.org/an-overhaul-of-type-classification/)、[现行类型规范](https://github.com/ourresearch/work-type-classifier/blob/main/WORK-TYPES.md)

旧文档明确把 Crossref 的 `journal-article`、`proceedings-article`、`posted-content` 归入过 `article`，以来源类型 `journal`、`conference` 和版本辅助区分，同时承认期刊与会议来源分类会出错。文档也承认旧 `review` 的覆盖不完整。因此不能通过排除 `review` 得到可靠的“原创研究论文”样本。[旧 Work 文档](https://github.com/ourresearch/openalex-docs/blob/main/api-entities/works/work-object/README.md#type)

## 建议保留三个可比较的文献集合

| 集合 | 定义建议 | 主要用途 |
|---|---|---|
| 正式论文主集合 | 正式期刊论文、综述、会议全文；排除通知、目录等非正文及仅预印本 | 跨国家、跨学科论文量与 ECI 主统计 |
| 期刊对照集合 | 主集合中具有期刊发表证据的部分 | 检查会议纳入对计算机、工程等学科的影响 |
| 扩展学术成果集合 | 主集合加经过确认的研究性书籍章节等，单独命名 | 检查人文社科及其他出版习惯的覆盖差异 |

这是本项目建议，不是唯一的计量研究标准。Leiden Traditional 使用 WoS 的 article、review，排除会议和书籍。Leiden Open Edition 2026 的广集合则涵盖 article、review、data paper、software paper、conference paper、book chapter，并加来源、参考文献、作者机构与撤稿限制。两者服务大学排序，不能直接移植为国家知识生产的定义。[Traditional 方法](https://traditional.leidenranking.com/information/indicators)、[Open Edition 方法](https://open.leidenranking.com/information/indicators)

## 逐类筛选建议

| 快照类型或情形 | 建议 | 需要补充判断的内容 |
|---|---|---|
| `article` | 主集合候选 | 结合正式发表来源及所有 locations 的版本证据；排查通知、目录、会议摘要、书评等误标 |
| `review` | 主集合候选 | 纳入文献综述与元分析；书评与审稿报告分开处理，不能视旧标签天然准确 |
| 会议全文，可能标作 `article` 或 `book-chapter` | 主集合候选 | 使用明确会议来源、上游 proceedings 类型或会议出版系列等证据；会议摘要排除 |
| `book-chapter` | 扩展集合；明确会议全文可进入主集合 | 普通研究章节、百科条目、会议章节分别识别，避免全收或全删 |
| `book-section`，字典外旧取值 | 复核后进入扩展集合；明确会议全文可另判 | 不直接等同研究章节或会议全文，需辨识章节正文、工具书分段及结构性记录 |
| `letter` | 待复核，默认不直接进入主集合 | 原创研究短文与读者来信区别很大；仅有标题含 Letter 不应排除 |
| `editorial` | 默认排除 | 若存在明确原创研究误标，进入单独复核，不按引用高低救回 |
| `erratum`、`retraction` | 排除通知本身 | 不能连带删除被更正的原文；撤稿通知与被撤稿论文是两类对象 |
| `preprint` | 不进入正式论文主集合，单列早期成果集合 | 与正式版的链接、重复及发表日期需要单独判断 |
| `book` | 扩展成果范围另议，默认不作为“论文” | 不应同时把整书和所有章节作为同一种论文单位相加 |
| `dissertation`、`report` | 默认排除，必要时单独成果集合 | 学位论文、技术报告和 working paper 与后续正式论文可能内容重合 |
| `dataset` | 排除数据实体 | 描述数据的正式论文仍可纳入；这份快照未单独提供 data-paper 标签 |
| `database`，字典外旧取值 | 排除数据库对象 | 正式数据库介绍论文可按其正文证据进入论文候选，不把数据库对象本身算论文 |
| `software`，字典外实际取值 | 排除软件实体 | 正式软件论文仍可作为 `article` 候选；不能因本快照没有 software-paper 就排除所有软件研究论文 |
| `grant`，字典外旧取值 | 排除资助项目对象 | 资助项目和项目产出的论文是不同计量单位 |
| `report-component`，字典外旧取值 | 扩展成果单列，默认不进入论文主集合 | 报告组成部分不能自动视作研究论文；避免报告整体与组成部分重复 |
| `peer-review` | 排除 | 正式审稿记录不等同综述论文 |
| `reference-entry`、`standard` | 排除 | 保持论文与工具书条目、技术标准的计量单位区分 |
| `paratext`、`libguides`、`supplementary-materials` | 排除 | 期刊封面、导航、附属材料不作为独立论文 |
| `other` | 默认排除，设复核池 | 只有明确的正文及发表证据才救回 |

上述边界以 OpenAlex 的类型规范、Crossref 记录类型及 Clarivate 文献类型定义为参考；快照旧标签的具体准确率仍须通过分层样本审阅估计。[OpenAlex 类型说明](https://help.openalex.org/data/work-types/)、[Crossref 记录类型](https://www.crossref.org/documentation/schema-library/markup-guide-record-types/)、[Clarivate 文献类型](https://webofscience.help.clarivate.com/Content/document-types.html)

## 精筛应采用的证据层次

1. 保留 `original_type`、原始来源及版本字段；输出独立的 `screened_class`、`decision`、`reason_code`，避免覆盖快照。
2. 高可信结构证据优先：通知类类型、上游细类型、明确的正式发表来源与位置、确定的预印本平台等。顶层没有 `raw_type`、`type_crossref`，但嵌套的 `primary_location.raw_type` 实际可用，可与 `version`、`is_published` 及所有 `locations` 的证据组合，不能将顶层缺字段误判为快照完全缺少上游类型。两文件共 402,439 条的说明性样本已出现 `proceedings-article` 等值，也发现 `type=review` 而 `primary_location.raw_type=dissertation` 的冲突。该样本不具代表性，只证明字段及冲突存在，不能外推总体错分率。[嵌套原始类型样本审计](snapshot-raw-type-sample.json)
3. 对有期刊或会议正式版的 Work，不因其同时具有 repository 或 submittedVersion 位置而剔除。应检查所有 locations，不能只检查 best OA location。[Locations 官方说明](https://help.openalex.org/data/locations/)
4. 标题关键词、页数、参考文献数量只作风险信号。比如“Index”可能是论文研究主题，短文可能是重要原创研究，会议全文与摘要也没有适用于全部学科的固定页数分界。
5. 不以 DOI、英文、摘要、可下载全文或正引用数作为全局纳入门槛；这些条件会把元数据可得性或既有影响力变成产出定义。零引用论文应保留。缺少已解析参考文献也不自动等于没有参考文献。
6. 将 XPAC 与文献类型作为两个独立维度：主统计可先用 core，扩展语料另做覆盖对照。core 并不等于同行评议认证，XPAC 也不等于“非论文”。官方将 expansion 描述为元数据总体更稀疏、更嘈杂的附加集合。[语料范围说明](https://help.openalex.org/data/works/corpus/)

## 去重和撤稿需要单独留痕

按 Work ID 计数后，同一 Work 的多个 locations 不再重复计数。对不同 Work 的潜在重复，不建议仅按相似标题自动合并；应优先使用 DOI、明确版本关系及作者等辅助证据，并输出合并理由。

Crossref 提供 `isPreprintOf` 连接预印本和正式版。OpenAlex 当前匹配规则也尝试合并版本，但其当前算法和合并状态不能反推本次旧快照已经无重复。会议全文与后续期刊论文可视为两个正式出版物；若要统计“独立研究项目”，则超出本项目的论文计数定义。[Crossref 预印本关系](https://www.crossref.org/documentation/schema-library/markup-guide-record-types/posted-content-includes-preprints/)、[OpenAlex 匹配与合并](https://help.openalex.org/data/works/matching/)

已撤稿原论文有两种合理处置，需用户审定。本项目主提案倾向保留其历史发表与被引并标记，另给排除 `is_retracted=true` 的敏感性版本；若目标更接近 Leiden 的未撤稿样本，也可将排除版设为主样本。`null` 应保留为撤稿状态未知。排除版表达的是“截至快照未标记撤稿的论文”，不是重建每个历史年份当时的撤稿状态。更正通知被排除不影响更正后的原文资格。

## 证据强度与局限

| 判断 | 证据强度 | 使用范围 |
|---|---|---|
| 快照早于25类型规范和7月重分类 | 强：快照元数据与官方带日期公告 | 确定需要适配旧分类；不等于已知道每条旧记录的正确类型 |
| 字典19类但Works实际24类 | 强：全量Works审计 | 类型完整性依据实际取值；字典外类别也要定义明确处置 |
| 嵌套原始类型可提供会议等细分类证据 | 强：schema与两文件402,439条说明性样本 | `primary_location.raw_type` 可用；与Work类型冲突应进入复核，样本比例不得外推 |
| 旧article混合期刊、会议等内容 | 强：官方旧文档及重分类说明 | 必须结合发表证据；具体错分率不能照搬官方其他日期样本 |
| article/review/会议全文可作为主样本 | 项目设计建议 | 服务跨学科覆盖；需要对会议摘要和正式版证据抽样验证 |
| 只保留期刊能增强可比性 | 有条件的方法选择 | 同时会改变会议及书籍依赖学科的覆盖，不能宣称普遍更准确 |
| 标题关键词和页数能帮助复核 | 辅助信号 | 不足以保证自动排除正确，特别是多语言标题和短研究通信 |
| 当前OpenAlex已合并同论文版本 | 规则层面有官方说明 | 不能据此认定2026年6月快照已无重复；快照中缺关系的记录仍需待定 |

此笔记提出的是可审阅规则和验证设计，尚未完成逐条文献判定，也没有修改远端原始数据。

## 给用户 review 的验证设计

首先输出全部实际类型、来源类型、语料来源的候选数量与筛选流失量；再按年代、学科、语言、元数据完整程度抽取可复现样本。会议摘要风险、旧 review、letter、repository 主位置、book-chapter、book-section、other 救回各设独立审阅分组；五个字典外类型也需单列覆盖检查。

每条 review 记录至少显示 Work ID、标题、年份、原类型、来源、版本证据、建议结果和触发原因；状态采用“纳入”“排除”“待定”，不把证据不足自动包装成已验证论文。

正式统计前应比较三个集合对国家产出份额、学科构成与 ECI 排名的影响。跨数据库研究发现，同一文献在 article、editorial 等类型上的分歧显著，因此不存在只靠一个数据库类型字段就完成精筛的保证。[原始研究 Haupka 等](https://arxiv.org/abs/2406.15154)

## 引用边界

筛选被统计论文集合与筛选“发出引用的文献”是两件事。直接累加候选论文的 `counts_by_year`，含义是这些论文当年收到的全部 OpenAlex 引用。若要求引用来源也仅限本项目主集合，必须重新遍历引用关系，不能声称 work 类型筛选已经实现。
