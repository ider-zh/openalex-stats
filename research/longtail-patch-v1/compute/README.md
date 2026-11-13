# 离线长尾配置计算记录

首发使用 **v2**：原／国家覆盖补充 × Core／All 共四组，每组 19 配置（baseline＋18 工程预设），每配置每方法 216 年度模型。底层矩阵与筛选规则为 `country-scale-cell-zero-monotone-diversity-v1`；方向策略为 `baseline-pooled-common-year-sign-v1`。完整定义和限制见[引擎说明](../../../docs/longtail-filter-engine.md)。

| 内容 | 记录 |
|---|---|
| 四组 Artifact SHA、原 baseline Float64-bit exact、JSON／Parquet 竞争排名审计 | `compute-audit-v2.json` |
| 本机解包后逐文件 SHA | `local-mirror-verification-v2.json` |
| 全部配置逐链 flip／weak／unavailable 汇总 | `direction-summary-v2.json` |
| 二进制、输入、源码、资源计划、模型数与限制 | `execution-provenance-v2.json` |
| 审计／打包、解包工具 | `audit_and_package.py`、`unpack_mirrors.py` |
| v1 原始方向及其案例／运输记录 | 无 `-v2` 后缀的原报告，原目录完整保留 |

229 正式候选计算目录：`/mnt/nvme0/public/openalex-stats/complexity-profiles-v2-20261113/{original-core,original-all,enriched-core,enriched-all}`。本机完整镜像：`bin/longtail-profiles-v2/` 下同名四目录；每组根 `manifest.json` 是唯一入口。大 bundle／运输 tar 不进入 Git。公开静态模型／压缩下载由发布团队从此镜像另行导出，HTTP／UI 数据隔离和 NumPy 数学独立核验另有验收记录。

每组 228 件被 manifest 引用的独立 Artifact 及 manifest 本身均已核验。所有 76 个筛选诊断文件和 8 个 baseline 模型 JSON 与 v1 **逐字节相同**，说明 v2 没有改变 X／M／准入或 baseline 实际分值，只增加明确、可审计的整条链参考方向。各链 ECI／PCI 共同乘一个因子，所用年份固定为 2000—2025，2026 不参与因子选择但跟随同一因子。

默认配置在 2025 年 Concepts 0 引用口径下排除 AG（优势学科不足）、EH（论文／引用支持量不足）、KP（论文支持量不足），SG 保留。增强 Core 该层保留原已知国家×已知学科 X 赋值 **99.8943%**、有效国家 146；它不是去重论文保留率、准确率或国家能力证明。增强 Core 的 Topics Field 论文模型保留赋值 **99.8203%**、有效国家 147；按完整年基准方向，SG 第 1，All 第 3。

Concepts 0 引用的原谱法与小倪方法、原／补充数据本就存在方向与结构语义差异。v2 各自参照同数据版、同方法的已发布 baseline，并不强制不同方法或不同数据版同号。完整详细名次和支持量见 `compute-audit-v2.json`；保留这些差异，不能把筛掉极端低支撑对象称为已验证的科研能力全球顺序。
