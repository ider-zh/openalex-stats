# 2026-11-13 研究归档（待 review）

本包是发布前研究归档，不作为网站运行时查询数据，也不覆盖原始结果；manifest 中未应用生产的标记记录归档时点。用户随后确认采用，r3 已另行导出为网站可切换的默认国家补全版，见[后续发布记录](../../docs/country-enriched-release.md)。冻结的补丁、汇总和哈希保持不变。完整解读见[小国家／地区高复杂度审查](../../docs/small-country-complexity-review.md)和[国家补全最终报告](../../docs/country-enrichment-v3-review.md)。

## 结果目录

| 内容 | 文件 |
|---|---|
| Core／All、完整年及含 2026 部分年的覆盖率 | `country/coverage.csv`、`coverage.json` |
| 严格文本国家补丁、范围、门禁与哈希 | `country/validated-text-patch.parquet`、`overlay-manifest.json`、`passed-gate.json` |
| 独立审计状态 | `country/final-r3-independent-audit-status.json` |
| 国家数量、学科变化及两法分值／名次变化 | `country/{core,all}/statistics/` |
| 同链统一变号的诊断对照（不修改正式结果） | `country/{core,all}/sign-diagnostic/` |
| 外部元数据失败／未核实情况 | `country/external-metadata-summary.json` |
| 原正式结果全年度 Top20、常规定义中位数汇总 | `complexity/all-year-*.csv.gz` |
| 32 个原始年度矩阵和原两法结果 | `complexity/representative-experiment-inputs.json.gz` |
| 18 场景 × 32 矩阵 × 2 方法，全部 1,152 组 | `complexity/experiments-r1/` |
| 独立 NumPy 与排名／相关／保留量审计 | `complexity/independent-matrix-audit.json`、`experiments-r1/independent-grid-audit.json` |

`manifest.json` 记录逐文件 SHA-256、未压缩 SHA、研究源路径和 229 上全量统计位置。CSV／JSON 的 `.gz` 是普通 gzip。完整统计 Parquet、原始署名、作者姓名、XML／PDF、人工盲审包与 Gold 留在研究服务器，不纳入此包。国家补丁只含 Work 标识、年、XPAC 标记、国家代码及补全通道／完整性状态，不含原署名文本。

覆盖率整数来自独立核对；比较 SQL 的极小浮点汇总舍入另保留在原 `review.json`，不要把约 `191419522.999996` 误读为分数篇。新增覆盖只表示原 UNKNOWN 获得至少一个标签，不等于完整合作国家集合已恢复。

严格文本原冻结精度留出为 255／256，核验依据是原快照身份与署名地址证据，并非全部取得出版者独立全文背书。保守延后 142 篇后未重新宣称独立条件精度。Crossref／PubMed／PMC／PDF 本批实际准入 **0**；候选、成功下载、已核实比例不能充当实际恢复量或整体准确率。

## 复核方法

从仓库根目录运行，Python 需 NumPy；从原全量统计重新提取矩阵还需 DuckDB 和 229 上原统计文件。以下将压缩输入和结果解包到独立工作目录，再运行只读审计：

```sh
python results/research-review-20261113/verify.py --extract tmp/research-review-20261113
python research/small-country-complexity-v1/independent_matrix_audit.py tmp/research-review-20261113/complexity/representative-experiment-inputs.json tmp/research-review-20261113/complexity/matrix-audit-rerun.json
python research/small-country-complexity-v1/independent_grid_audit.py tmp/research-review-20261113/complexity/experiments-r1
go run ./research/small-country-complexity-v1/experiments --input tmp/research-review-20261113/complexity/representative-experiment-inputs.json --out tmp/complexity-experiments-rerun --workers 32
```

独立实验源码在 `research/small-country-complexity-v1/{diagnostic,experiments}`；网络阈值只用于研究，不是上线规则。默认输入先逐项验证原 M，并核对两法原分值到符号／数值容差；原符号及同年比较符号同时保存，不能挑选符合预期的榜单替换原版。

国家重算、门禁合并和独立最终审计工具在 `research/country-enrichment-v3/{recompute,merge,validation}`。`recompute` 与共享 mmap 索引面向 Linux、原 CSR／引用计数和 snapshot；完整重跑需要相应原文件。原始源材料和 Gold 不公开，所以 Git 包能独立复核已发布聚合矩阵的数学计算，不能单独重建所有人工来源证据。外部元数据工具的源、身份约束、用量、许可归属与复现要求见 `research/country-enrichment-v3/metadata/REPRODUCE.md` 和 `SOURCE-ATTRIBUTION.md`。
