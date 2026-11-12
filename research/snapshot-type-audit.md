# OpenAlex 快照文献类型审计

本次只读审计针对 229 的 `/mnt/hg02/openalex-snapshot/data/parquet`，manifest 快照日期为 **2026-06-26**。未修改源数据，未实施逐条全量精筛。

## 全量结果

- 扫描全部 **510,372,821** 条 Works；核心语料 `is_xpac=false` 为 **317,820,190** 条，扩展语料为 **192,552,631** 条。三者均为本次实测。
- `type × is_xpac` 查询耗时 **140.48 秒**；核心语料 `type × primary_location.source.type` 查询耗时 **50.66 秒**。每条查询线程数 4、内存上限 8GB，仅投影必要列。
- 核心语料 `article` **218,098,661** 条，`review` **4,154,173** 条，合计 **222,252,834** 条。这是**宽口径候选规模，不是精筛后论文规模**。
- 这些 article/review 候选中，`is_retracted=true` **92,570** 条。仅减去这些已标记撤稿记录后为 **222,160,264** 条，仍不是精筛结果，亦不说明未标记者都未撤稿。
- 本次全量统计中，只有 `type=paratext` 记录的 `is_paratext=true`。article/review 内该标记为 true 的记录数为 0，因此这个布尔过滤不能进一步清掉它们内部的书评、会议摘要等内容。

## 字典与实际值不一致

`work-types` 实体目录列出 19 类，但实际 `works.type` 有 **24 类**；额外出现 `book-section`、`database`、`grant`、`report-component`、`software`。因此规则要对未列举类型有明确的待复核/排除分支，不可只信字典行数。

`primary_location.source.type` 也有字典常见值以外的 `metadata`、`igsnCatalog`、`raidRegistry` 等实际值。应保留原始值并把未知来源送入审计，不能默认为 journal。快照没有 `type_crossref` 顶层字段，原始类型路径是 **`primary_location.raw_type`**；另有 `primary_location.version`、`is_published` 等辅助字段。

## 来源不是论文类型的替代品

核心 article+review 候选按主来源类型：

| 主来源类型 | 记录数 |
|---|---:|
| journal | 158,759,111 |
| repository | 33,683,354 |
| NULL | 27,144,093 |
| conference | 2,186,877 |
| book series | 424,919 |
| ebook platform | 49,493 |
| other | 4,965 |
| metadata | 20 |
| igsnCatalog | 2 |

只保留 `source.type=journal` 将大量候选排除，其中包含可能的正式发表论文。反过来，journal 来源也包含书评、社论等内容。来源类型及 `publishedVersion` 均不是经过同行评审的保证。

## 有目的的样本检查

另选最早更新分区及最新更新分区各一个文件，合计 **402,439** 条 Works，其中核心语料 **201,578** 条。该样本不是随机样本，不能用于估计全库污染率。原始分组 836 行，按类型/来源/原始类型选取的原始示例 380 条；另提供约 60 条精简用户审阅记录。

实际观察到：

- `article + raw_type=proceedings-article` 出现在 source 为 conference、journal 及 NULL 的记录中。识别会议论文不能只用主来源类型。
- `article` 中有 `raw_type=Conference Abstract`、`Abstract`、`editorial`、`Letter`，以及标题直接写明 Book review 的记录。
- `review` 中既有系统综述，也有标题明确的书评，以及 raw_type=dissertation 等冲突记录。不能把所有 review 都当成综述论文。
- `raw_type` 很杂，部分是 `Article / Letter to the editor`、`editorial reviewed` 等复合或非类型描述。不能用包含 editorial/letter/review 的简单子串直接自动剔除。

## 建议的筛选顺序

1. 以实际 type 清单分流；article/review 作为候选，会议全文从原始类型及其他位置证据辅助识别；book-chapter、letter 等进入单独复核分支。
2. 对明确的会议摘要、海报、书评、社论、勘误、撤稿通知等进行有证据的排除；信息冲突记录进入 review 队列。
3. 显式区分：主来源位于 repository 与整篇作品是 preprint；前者不能直接推断后者。
4. 不以 DOI、摘要、全文可得性、单纯页数阈值作为所有国家/学科通用硬门槛。
5. 经用户审阅后，再把规则应用到全库并统计每条规则的命中数及国家/学科差异。目前未完成这些逐条精筛步骤。

## 复现与审阅文件

- `snapshot-type-audit.py` / `.json`：全量查询、原始 schema、类型分布、核心来源分布和第一组示例。
- `snapshot-type-audit-summary.json`：关键规模和候选来源分布。
- `snapshot-raw-type-sample.py` / `.json`：两文件抽样脚本、原始类型分组和完整样例。
- `snapshot-document-review-sample.json`：精简逐条审阅记录，带标题、年份、work ID、type、source、raw_type、version 和暂定审阅理由。
