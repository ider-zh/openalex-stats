# 国家信息版本选择

国家信息版本与 Core／含 XPAC 全数据、Topics／Concepts、统计口径和知识复杂度方法分别选择。新版默认使用 **国家补全版**；选择 **原始版**可对照原快照国家归属。

页面在语料版本下提供单击切换按钮，并在结果状态、图表口径与说明中显示当前国家版本。“补全方法与覆盖变化”可展开页底说明并定位到相应内容。首次导引同时介绍该选择。

## URL 与数据绑定

- 稳定参数为 `country_version=enriched|original`。没有参数时使用服务声明的默认版本；正式双版本发布为 `enriched`。显式 `original` 的链接与刷新保留原版。
- 切换国家版本保留语料、方法、学科、国家、指标、计算基础、时间与场景参数，重新载入完整页面。切换 Core／All 同样保留国家版本。
- 在声明国家版本的服务上，元数据、曲线、曲线 CSV、原始产物、模型产物及轮数敏感性报告均显式携带该参数。模型产物另保留 `method`，所有曲线和全球名次来自同一国家版本的统计／模型。
- 前端核对多版本服务返回的 `meta.country_version` 与 `series.query.country_version`。回显与所选版本不符时不画图，显示错误；避免将原版数据标为补全版。
- 旧服务若没有声明 `catalog.country_versions`，界面只提供原始版，不把旧数据默认为补全结果；对旧 API 不发送其不认识的国家版本参数。国家版本不写入 localStorage；当前明确 URL 是分享与刷新时的依据，首次无参数访问仍按服务默认。

API 契约：`/api/catalog` 保留 `datasets` 与 `default_corpus`，同时提供 `country_versions` 和 `default_country_version`；`/api/meta` 回显 `country_version`。补全版不可变批次标识可由 `country_version_info.version` 记录，与公开选择参数分开。

## 方法说明的范围

本批只为原国家为空的论文补入严格署名文本识别国家，按补入后的归属重新统计。原来已有非空国家的误配尚未全面修正；Crossref／PubMed／开放全文候选本批未通过推广条件，未纳入补全版。

页底展示 2000—2025 完整年覆盖率：Core **49.3127%→50.3009%**，All **36.8614%→37.5577%**，并说明至少一个国家的覆盖不同于完整合作国家集合。255／256 的留出核验说明同时披露原快照证据范围、Wilson 区间和整体国家准确率的区别，不把它作为全部标签可靠性的保证。

## 验证

```sh
QA_URL=http://127.0.0.1:8898 QA_OUTPUT=research/country-enriched-release-v1/ui node scripts/country-version-ui-test.mjs
```

该实服务浏览器检查覆盖默认版、显式原版刷新、Core／All 与国家版本的交叉选择、两种方法下 ECI／PCI、CSV 版本字段、下载链接绑定、移动端说明和布局，以及故意错版响应被前端拒绝的行为。截图和机器检查结果保存在 `QA_OUTPUT`。脚本不运行模拟统计，不修改源结果。

2026-11-13 三个真实环境均通过同一套 10 组浏览器检查：

| 环境 | 验证结果 |
|---|---|
| 229 Go 候选 8898 | `research/country-enriched-release-v1/ui/go-candidate-r2/country-version-ui-qa.json` |
| Cloudflare 实际 dist 本地预览 8798 | `research/country-enriched-release-v1/ui/pages-r1/country-version-ui-qa.json` |
| 229 正式服务 8899 | `research/country-enriched-release-v1/ui/production-r3/country-version-ui-qa.json` |

每个环境核对了 Core／All × 原始／补全 × 小倪／谱法 × ECI／PCI 的 16 个组合，图表分数／全球名次逐点与所选版本 API 对照；两个语料的原／补全元数据具有相同圈定论文及引用图，补全版缺国家论文数下降。手机宽 390px，无横向溢出，版本按钮字号至少 14px。没有页面脚本错误或站外浏览器请求，且故意制造的错版响应被拒绝，未画入旧图。

上述三次均未覆盖浏览器资产，直接测试各自实际服务／构建内的资源。原始统计产物、模型与 source run 的一致性另由数据发布审计核验；本浏览器脚本不代替源数据质量和方法有效性审查。每个环境保留桌面与手机截图。早期原服务资产覆盖试验未通过，不作为本次发布验收依据。
