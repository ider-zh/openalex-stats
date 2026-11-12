# 外部元数据国家补全 v3：最终结论（2026-11-13）

**本批 Crossref、PubMed 实际新增准入为 0。** 完整来源、全量候选、预 Gold 角色隔离和独立整体/净域验证均已完成；两条外部路线均未通过预声明门禁。原快照、cache、原 country 版本、引用图和网站没有被本 metadata 路线改写。没有生成空的伪 passed 补丁，也没有扩大、合并失败后的样本。

Root 最终可应用的严格文本 `text-final-r3` 是 **2,667,126 个 Q Work**；Core 2000—2025 年恢复 1,891,570 个 Work，国家覆盖率 50.300920%。这些是文本路线及其最终保守范围收缩的成果，不归功于未准入的外部数据；完整统计差异由 Root 总报告给出。

## 独立验证结果

| 路线 / 域 | 独立论文簇 | 联合核实支持 | 确认不支持 | 论文身份未核实 | 95% Wilson 下界 | 结论 |
|---|---:|---:|---:|---:|---:|---|
| Crossref 整体 | 128 | 56 | 1 | 71 | 35.4623% | 不准入 |
| Crossref 净新增 | 128 | 48 | 0 | 80 | 29.5924% | 不准入 |
| PubMed 整体 | 128 | 127 | 1 | 0 | 95.7074% | 不准入 |
| PubMed 净新增 | 128 | 127 | 1 | 0 | 95.7074% | 不准入 |

Crossref 的 56/128、48/128 是严格证据门禁下的**核实成功比例**，不是实际准确率；未取得独立论文题名/年份证据不能称为已证实错误，也不能用 ORCID+同 DOI 自证同篇论文。已确认的一例是加拿大机构地址误写 USA。

PubMed 两个独立域各自 127/128，样本联合精度点估计均为 **99.21875%**，各自双侧 95% Wilson 区间为 **[95.707374%, 99.861957%]**。点估计满足 ≥99%，但下界不满足预声明的 ≥97%，因此必须不准入。两域分别发生校园邮政转运国误作实际所在地、加拿大机构地址字面 USA 污染。不能事后把两个域合并成 256 个 IID 样本或扩抽来获得通过。

所有原抽样、原 Gold、失败分母、原冻结引擎结果和原阶段版本保留。143 个最终应用 defer 是原 FH 错误 1 个，加 78+50+14 个已确认邮政/地理范围风险；它们只保守收缩最终应用，不删除原验证失败、不自动改成 CY/CA，也不构成一次新的独立准确率验证。原始已知 country 标签的准确率不由本次新增标签验证推导。

## 完整来源与候选

| 项目 | Crossref | PubMed |
|---|---:|---:|
| 固定来源 | research-organisation assertions v2，内容截至 2025-03 | 2026 年 1 月完整 baseline，未应用 daily updates |
| 原文件 | 4,421,007,134 bytes，复用缓存 | 1,334 文件，54,267,874,919 bytes |
| 源记录 | 140,906,929 断言 | 39,994,988 PubmedArticle |
| 来源准备 | 12,926,936 个人 contributor/ORCID/显式国家断言 | 22,380,087 含显式国家的本文记录 |
| 精确标识符潜在命中 | 75,846 Work / 188,966 断言 | 548,022 Work / 548,634 源记录 |
| 原冻结引擎自动候选 | 38,995 Work | 106,967 Work |
| 预 Gold Collective-only 整 Work 隔离 | — | 82 Work |
| 个人域候选 | 38,995 Work | 106,885 Work |
| 独立整体抽样框，排开发论文后 | 38,990 Work / 38,976 cluster | 106,861 Work / 106,821 cluster |
| 独立净域抽样框，排实际 passed text 和开发论文后 | 330 Work / 330 cluster | 25,421 Work / 25,416 cluster |
| 本批实际应用 | **0** | **0** |

共同原 UNKNOWN 总体为 277,418,468 个合格 Q Work，其中 Core 150,377,414、XPAC 独有 127,041,054；保留预 2000 年被引目标。全部 2,446 个原始分片的标识符投影逐行核 ID/year/type/XPAC。联合原作者首版 779,238 Work，完整 PubMed 后增量到 843,899，仅新增 64,661；首版保持不动供并行 PMC 审查复用。PubMed 新下载 1,129 文件 / 47,226,633,517 bytes，其余 205 文件复用旧缓存；完整源官方 MD5、接收 SHA 和解析源 SHA 闭合。

潜在命中、自动候选、净域抽样框和实际覆盖增益是不同量。PubMed 只扣 text、尚未排开发论文时的潜在净新增是 25,428 Work，但这批没有通过门禁，因此实际新增仍是 0；对论文数、原范围当年被引、RCA/ECI/PCI 的外部数据额外改变量也是 0，不是指标本身等于 0。

## 规则与预 Gold 角色修复

Crossref 必须个人 author contributor-affiliation、同 DOI、有效 ORCID 和同一原 authorship 的原始姓名同时吻合；不以 profile ORCID 覆盖冲突原名，不自动使用机构作者、机器 ROR 或出版机构总部。专用 dump 没有论文题名/年份，须独立补论文身份。PubMed 必须同 PMID/DOI且无非空标识符冲突、长题名规范化完全一致、预定 +/-1 年窗口、原作者支持；只取本文署名 affiliation 和既定 limited legacy Article Affiliation，不取期刊出版国或参考 DOI。

静态 code review 在 PubMed Gold 之前发现原 adapter 丢了 CollectiveName 角色。原 binary、原 raw outcomes、原候选完整保留，另冻结整 Work quarantine：任一提议国家仅有集体署名支持而无个人或既定 limited legacy 支持，整 Work 隔离，不偷偷删国家伪装原输出。角色视图只清 affiliation，所有作者名字/ORCID和身份字段不变，所有国家仍由同 SHA 原 Go binary计算。5 合成场景/6 记录通过；实际 103 记录/206 视图身份与国家并集闭合，82 Work 隔离，19 个人+集体同国保留；独立 validator 已全量复核。国家集合完整性仍单列 unknown/false，不能把部分来源的国家并集当全部作者国家已齐。

独立验证按规范 DOI/PMID/PMCID 连通分量抽 128 cluster，审核全部成员；任一错误/未核实则 cluster 失败。整体与最终净域分别门禁，预定 point≥99%、95% Wilson lower≥97%，成员 Work point≥99%；未核实留分母，不替换。原概率样本不改。恢复优先级及本批 text-only 净域账本保持冻结，失败的外部来源不进入恢复排除集。

## 标识符风险的只读诊断

PubMed 548,634 条连接源记录中，387,905 条因题名门槛拒绝：348,630 仅规范化题名不同，31,100 同时不满足长度/词数且不同，8,175 仅长度/词数不足。另有 49,253 条原作者不支持、2,371 DOI conflict、462 PMID conflict、1,676 年份未支持。

其中 **357,030 条原 Work 数值 ID 恰等独立 source PMID**，356,024 被题名规则拒绝，356,018 的规范化题名确实不同，只有 62 条仍 eligible。这是需要另行核对的标识符/namespace 关联风险指示器，不能把所有题名差异说成错误论文：翻译、短题名、格式、版本也可能触发；没有转换链审计就不归责 OpenAlex、PubMed 或任何上游服务。本轮不修改 DOI/原 Work/引用图，也不绕过题名检查填国家。

旧三例 W19239315、W26184156、W34102155 在完整源中各命中一篇不同 PubMed 论文，全部由冻结 `strict_long_title_not_equal` 拒绝；W26184156 的一条 Crossref 断言也因原名+ORCID不支持拒绝。三个已知错误均无自动候选，原证据和 mandatory quarantine 保留。

## 成本与工程边界

| 阶段 | wall / 执行记录 |
|---|---|
| PubMed 完整源准备 | 5,837.8 秒，包含等下载，不是纯 CPU 时间 |
| 首版联合原作者读取 | 3,006.21 秒，779,238 Work / 2,185 parts |
| 完整源增量原作者读取 | 835.55 秒，843,899 Work / 2,188 parts，旧版硬链接复用 |
| 完整 PubMed 潜在匹配 hash join | 63.73 秒 |
| PubMed adapter+Go引擎+聚合 | 约260.03秒，其中Go outcomes输出区间约96秒；时间戳近似 |
| 个人角色 scope | 约48秒，103记录Go视图输出<1秒 |
| 整体128原XML /121源 | 48本地Go gzip workers，10.53秒 |
| 净域128原XML /108源 | 48本地Go gzip workers，6.38秒 |

CPU/RAM“90%”没有凭 worker 数冒充实测利用率。慢阶段主要受外部运输和原始 HDD 分片读取限制；没有为凑占用无证据加并发。稠密标识符列投影保留实测更快的 DuckDB；原作者 EXPLAIN 显示整 authorships 扫描，窄 Go 省 IO但高 target 时更费CPU，未冒险重启已运行任务。隔离 Go MAP buffer 基准的跨 batch 残留已被发现并拒绝，未进入共享权威结果；清零修复后174,129行14字段全闭合。原作者另与独立安全Go稀疏读取43个Work四叶和顺序全闭合。

下载长连接降速后改6路成熟curl断点续传，整文件官方MD5+SHA通过后原子发布；一次截断仅补105,581尾字节，原失败凭证保留。没有逐DOI/PMID元数据API，没有把未到源当不存在。Crossref一次177 DOI公开格式补证只有4 HTTP200原响应，其余403/404/无公开路线/错误均保留；HTTP200本身也不当论文身份通过。

## 复核与公开范围

机器结论见 `final-summary.json`；题名/拒绝描述见 `pubmed-title-diagnostic.json`、`pubmed-decision-summary.json`；规则及角色隔离见 `policy-v2.json`、`personal-country-scope-v1.json`；来源署名见 `SOURCE-ATTRIBUTION.md`；重跑边界见 `REPRODUCE.md`。完整原断言、作者、XML/PDF、私有 inputs/outcomes、盲包和Gold证据只留原研究目录，不进入Git。建议公开路径以 `publicFiles.json` 的显式白名单为准。
