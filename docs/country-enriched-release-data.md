# 国家覆盖补充版本：发布数据审计

核验日期：2026-11-13。本次用户已授权把国家补全结果接入网站，保留原始版并默认选择补充版。本文核验可发布数据与质量口径；网页、API／CSV、下载和 Pages 构建的版本隔离由发布验收另外验证，不把仅有源文件审计称作已完成网站部署。

## 数据身份

建议不可变数据版本标识为 `country-enriched-v3-r3`，界面名称“国家覆盖补充”；原版标识 `original`，名称“原始国家信息”。Core／All 是另一项独立语料选择，复杂度方法仍分别保留 `spectral-v1` 与 `java-reflections-v1`（小倪方法论），不能把补全版本混作第三种复杂度算法。

229 路径共同前缀为 `/mnt/nvme0/public/openalex-stats/results/`。

| 语料／版本 | 基础统计目录 | 小倪方法侧车目录 | run ID |
|---|---|---|---|
| Core 原始 | `core-concept-hits-v1` | `core-java-reflections-v1` | `4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455` |
| Core 补充 | `core-country-validated-v3-r3-20261113` | `core-country-java-v3-r3-20261113` | `90af69cb5c5416dd34fcde73df461469133a43d9d692e5984f2e4f49c2edb18e` |
| All 原始 | `all-concept-hits-v1` | `all-java-reflections-v1` | `dd679f119b4d24a9d6725e79dec96af9d731265de59844ae04bbd028a719153e` |
| All 补充 | `all-country-validated-v3-r3-20261113` | `all-country-java-v3-r3-20261113` | `567dea9f8acaff29eae40bf3ed0c5b415eb34633222df9e2bface813b759ce2d` |

最终补丁 SHA-256：`fe6d3abaf6d94e5ca3c209c6a5e5270f7c8c85964dc34ac3d3ce9e6c662db5f3`。仅准入冻结的严格署名文本路线，共 2,667,126 篇，Core 2,635,485 篇；包含 2000 年以前合格的被引对象。142 篇地理／邮政地址有歧义的 Work 整篇延后，原失败样本保留。Crossref／PubMed／PMC／PDF 外部路线实际准入 0。

**这里是补原 UNKNOWN，不是修复所有国家错误。** 原有非空国家集合未扩张、未删除或替换；前述安提瓜／朝鲜等已知原标签误配并未因切换此版本而自动修正。界面宜明确写“国家覆盖补充”，避免“全面修正国家版”的表述。

## 完整年与部分年的实测覆盖

论文覆盖分母是圈定范围内在所列年份发表的去重 Work，分子为至少有一个国家的 Work；不把多国全计数或多 Concepts 命中求和充当论文分母。引用分母是所列施引年份的合法引用事件，按被引目标国家归属，包含更早发表的目标。

| 语料 | 输出年份 | 原论文国家覆盖率 | 补充后 | 恢复论文数 | 原引用国家覆盖率 | 补充后 | 恢复可归属引用事件 |
|---|---|---:|---:|---:|---:|---:|---:|
| Core | 2000—2025 | 49.3127% | 50.3009% | 1,891,570 | 88.0872% | 88.9309% | 19,328,116 |
| All | 2000—2025 | 36.8614% | 37.5577% | 1,918,210 | 87.9648% | 88.8031% | 19,384,756 |
| Core | 2000—2026 | 48.6875% | 49.6430% | 1,923,240 | 88.3201% | 89.1540% | 19,707,840 |
| All | 2000—2026 | 36.8502% | 37.5335% | 1,949,924 | 88.1983% | 89.0270% | 19,764,899 |

2026 为部分年。逐年 2000—2026、Core／All、原始／补充各版合计 108 行的精确整数总量及覆盖率，见 [annual-country-coverage.json](../research/country-enriched-release-v1/audit/annual-country-coverage.json) 和 [CSV](../research/country-enriched-release-v1/audit/annual-country-coverage.csv)。从国家均分、Topics primary Field 含 UNKNOWN 学科的统计求和，逐年量守恒且可归属量不减；整数化前浮点残差均低于 0.001。原质量字段本身使用精确整数，两者一致。

## 兼容性与发布前守卫

2026-11-13 只读重核了四组 base＋method completion 拥有的全部 **76 件产物**，大小及 SHA-256 全部通过。每组方法的 `base_run_id`、源 completion SHA、method manifest SHA 与基础统计严格绑定；不使用原版的侧车配增强统计。四组基础统计均为 `full_snapshot_complete`，六统计场景、四分类层、两种指标完整；每组两种方法的国家／学科分值 Parquet 都覆盖 216 个年度模型。

源已包含 `statistics.parquet`、`cube.parquet`、`rca.parquet`、`coverage.json`、字典／场景、`eci.json`，及小倪方法 JSON、分值 Parquet、独立审计和 12/13、16/17、20/21 轮敏感性产物。原始全量重算及独立数值审计已完成，本次没有再次扫描快照或重跑模型。

发布须保持以下边界：

1. 数据版本、语料、复杂度方法在 API、图表、CSV、下载中一致；查询不能拿另一版本的分母或全局排名。切换保持国家、学科及年份选择，仅采用所选版本真实结果。
2. 默认补充版；明确原版仍可选。旧链接或缓存的显式版本选择不得悄悄被默认覆盖。
3. 补充版必须导出全部国家、年、层、场景和两种方法，不能只导出默认九国。Pages 逐片验证 SHA、文件数与单文件体积；完整源文件过大时继续用独立 Release 资产，下载目标须与所选版本一致。
4. 源侧车旧 display name 仍为历史记录；当前应用已用方法 ID 映射“小倪方法论”，无需为改名改动冻结模型文件。计算阶段 `production_applied=false` 是不可变的当时来源记录，不应直接当作发布后状态；上线状态应由新的发布 manifest 记录。
5. Core／All Concepts 0 引用谱方法的原／补充全链方向相反，已由既定弱符号锚点差异解释。保留既定正式分值，解释版本切换中的镜像排名；不把所有差异宣称为补全带来的科研能力变化。详见[前次最终审查](country-enrichment-v3-review.md)。

## 补充方法说明建议

> 国家覆盖补充：仅对原国家为空的文献，从原始署名单位文本提取明确国家证据，使用冻结的严格规则并排除冲突、歧义地址；保留原有非空国家。该路线原独立留出 256 篇中 255 篇得到来源证据支持（99.61%，95% Wilson 区间 97.82%—99.93%）。这是候选补全通道的源证据核验，不是全部国家标签准确率；并非全部样本取得出版者独立全文背书。覆盖提升表示至少识别到一个国家，仍可能缺少合作国家或存在原有误配。

中文说明、验证范围和两窗口精确量另存 [release-summary.json](../research/country-enriched-release-v1/audit/release-summary.json)，供应用按需要采用。公开汇总不包含作者、原始署名、逐篇外部元数据或全文。

完整源路径、76 件校验、run／method 身份、原始 quality 与场景记录见 [manifest.json](../research/country-enriched-release-v1/audit/manifest.json)；可复算脚本见 [audit_release.py](../research/country-enriched-release-v1/audit/audit_release.py)。远程只写新研究目录 `analysis-country-enriched-release-v1-20261113/{audit-r1,release-packages-r1}`，未改原结果或运行服务。

## 原始结果下载包准备

增强版四组 result／method 的 completion 拥有文件加各自 completion 共 **42 件、2,765,833,596 字节**，已从229独立运输包下载到本机 `bin/country-enriched-r3-results/{core-base,all-base,core-methods,all-methods}`，逐件大小／SHA 再核对。最大单件 493,217,314 字节，没有超过 GitHub Release 2GiB 的单资产上限。没有复制原作者／原署名／外部全文；`bin` 镜像及运输包不进入 Git。

准备阶段公共索引为 [country-enriched-v3-r3-index.json](../results/country-enriched-v3-r3-index.json)，拟用 Release tag `snapshot-20260626-country-enriched-r3`。资源名严格对应静态导出器的 `core-{filename}`、`all-{filename}`、`core-methods-{filename}`、`all-methods-{filename}`。本机上传列表 `research/country-enriched-release-v1/audit/release-upload.json` 含绝对源路径，供发布进程使用，不必公开。准备脚本 [prepare_local_release.py](../research/country-enriched-release-v1/audit/prepare_local_release.py) 不读取 GitHub 凭据、不发网络发布请求；只有正式上传及远端 SHA 核验通过后，发布流程才能把公共索引从 `prepared` 标为 `published`。

## 增强版真实静态导出全值核验

使用独立 `scripts/verify-static-export` 从增强版原 Parquet、原谱／小倪模型读取全部值，自行解码 OAQ1 并独立排序竞争排名，没有复用服务查询适配器。对真实 `static-enriched-r1` 导出核验 **7,133 件静态文件 SHA、30,064,114 个原计数单元格、全部完整分母、864 个模型及 339,768 个模型点**，Float64 位值、状态和全球竞争排名差异全部为 0。

该核验绑定 r3 四组 base／method completion 和 42 件发布资产集合；工具 SHA-256 为 `a5d7d277507ab4f09033beb7669f72f31867c7ff5978aa71c25920a180c6cd5b`，静态 export manifest SHA-256 为 `8bdf5687a8f42f5791760f17b78deb59f52b0f1b76ebe3557a90d8c1cb61a5f4`。小报告见 [enriched-static-verification.json](../research/country-enriched-release-v1/audit/enriched-static-verification.json)，执行与输入身份见 [provenance](../research/country-enriched-release-v1/audit/enriched-static-verification-provenance.json)。这证明增强版科学值的静态运输一致，不替代四数据集合并后的 API／CSV／下载／界面切换验收；原始版保留已有独立全值审计。
