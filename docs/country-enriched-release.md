# 国家补全版：网站双版本发布

2026-11-13 发布。国家数据版本独立于 Core／含 XPAC 全数据和复杂度方法，网站默认使用**国家补全版**；单击**原始版**可查看原快照国家归属。显式原版分享链接保持原版，切换版本保留当前年份、国家、学科、指标、场景与方法。

## 发布数据

| 数据范围 | 原始版 run ID | 国家补全版 run ID |
|---|---|---|
| Core | `4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455` | `90af69cb5c5416dd34fcde73df461469133a43d9d692e5984f2e4f49c2edb18e` |
| 含 XPAC 全数据 | `dd679f119b4d24a9d6725e79dec96af9d731265de59844ae04bbd028a719153e` | `567dea9f8acaff29eae40bf3ed0c5b415eb34633222df9e2bface813b759ce2d` |

两版各自提供完整计数、份额、RCA、四个分类层级的小倪方法论与谱方法模型及全球排名。两种复杂度方法仍可切换，小倪方法论保持默认。每个方法 sidecar 都校验匹配的 base run 和 completion SHA；不会用原版模型搭配补全版数量。

| 2000—2025 完整年 | 原国家覆盖率 | 补全后 | 新增有国家标签的论文 | 新增可归属引用事件 |
|---|---:|---:|---:|---:|
| Core | 49.3127% | 50.3009% | 1,891,570 | 19,328,116 |
| 含 XPAC 全数据 | 36.8614% | 37.5577% | 1,918,210 | 19,384,756 |

上述覆盖每篇按是否至少有一个国家计一次。页面顶部用完整输出年 2000—2026（含部分年）的质量字段，因此补全版 Core 顶部显示约 **49.64%**、All 约 **37.53%**；展开说明中的 **50.30%／37.56%** 对应截至 2025 的完整年，两处年段不同。逐年、两版、两语料共 108 行覆盖率见[年度汇总](../research/country-enriched-release-v1/audit/annual-country-coverage.csv)。

本批只为原国家为空的论文采用冻结的严格署名文本国家证据；全 Q 补丁 2,667,126 篇，含更早合格被引目标。已有非空国家集合不替换、不扩张，原有误配尚未全面修正。Crossref／PubMed／PMC／PDF 本批实际准入为 0。原留出 255／256 是原身份与署名地址证据核验，并非全部原国家标签准确率，亦不保证合作国家集合完整。142 篇歧义地点／邮政地址暂缓补入。详细范围见[研究 review](country-enrichment-v3-review.md)。

补入国家会改变 RCA 网络和复杂度模型；跨版本的整体符号方向变化不能直接解释为科研能力变化。现有结果保持各版既定方向，未为使榜单符合预期调分、翻号或删去小国家。解释与敏感性证据见[小国家高排名审查](small-country-complexity-review.md)。

## 接口与下载

稳定参数为 `country_version=enriched|original`。API 元数据、曲线、CSV、完整产物、方法模型和敏感性下载都绑定所选国家版本；JSON 和响应头回显版本，CSV 增加同名列。前端若收到不同版本则拒绝画图，避免标签与结果不符。

原始结果 Release 不变；增强版另存[国家补全 r3 Release](https://github.com/ider-zh/openalex-stats/releases/tag/snapshot-20260626-country-enriched-r3)，42 件资产共 2,765,833,596 字节，逐件核对本机 SHA 和 GitHub 上传 digest。[增强版公开下载索引](../results/country-enriched-v3-r3-index.json)记录每个文件的大小、SHA 和 URL。

```sh
node scripts/download-results.mjs --corpus core --country-version enriched
node scripts/download-results.mjs --corpus all --country-version enriched
```

增强版默认写入 `results/full/enriched/{core,all}/{base,methods}`；CLI 省略国家版本仍保持历史原始版默认，网页默认增强版。两类文件不覆盖同一个下载目录。

Go 服务同时加载四组不可变结果：

```sh
./oa-stats serve --listen 127.0.0.1:8899 \
  --data-core /results/core-concept-hits-v1 \
  --data-all /results/all-concept-hits-v1 \
  --methods-core /results/core-java-reflections-v1 \
  --methods-all /results/all-java-reflections-v1 \
  --data-core-enriched /results/core-country-validated-v3-r3-20261113 \
  --data-all-enriched /results/all-country-validated-v3-r3-20261113 \
  --methods-core-enriched /results/core-country-java-v3-r3-20261113 \
  --methods-all-enriched /results/all-country-java-v3-r3-20261113
```

大数据启动需要加载时间。发布时可加 `--wait-for-listen 15m`，先在旧服务仍运行时加载新数据，待日志确认已加载后再交接端口；只对占用端口重试，其他监听错误立即失败。旧二进制、启动参数和所有原始结果保留。

Cloudflare 使用同一界面与 Go 生成分片。双版本合并后 14,265 件数据文件、388,352,283 字节；完整网站 14,531 文件、391,192,319 字节，最大文件 4,062,388 字节。构建逐件核 SHA，符合当前构建器使用的 20,000 文件／25 MiB 单文件检查。原版 7,132 件科学数据文件字节未改；只增加增强版分片并更新控制清单。部署方式见[Cloudflare 双版本说明](country-enriched-cloudflare.md)。

## 验证证据

- [发布前审计](country-enriched-release-data.md)：四数据集 76 件产物大小／SHA、方法来源与年度覆盖通过。
- [增强导出独立全值审计](../research/country-enriched-release-v1/audit/enriched-static-verification.json)：7,133 件 SHA、30,064,114 个计数格、全部分母、864 模型及 339,768 个分值／状态／独立竞争排名，差异为 0。
- [Pages／Go API 对照](../research/country-enriched-release-v1/parity/pages-parity.json)：1,984 查询、667,008 点值、56 CSV、48 小产物、32 大产物 HEAD 与最终 Release HEAD 一致；保留全国家全球排名。
- [官方 workerd 对照](../research/country-enriched-release-v1/parity/workerd-parity.json)：同一完整查询／CSV／下载集合再次通过，无 ASSETS 或运行时错误。
- [Go 候选浏览器验收](../research/country-enriched-release-v1/ui/go-candidate-r2/country-version-ui-qa.json)：默认／原版、两语料两方法、版本切换、模型／CSV／下载和 390px 布局通过。
- [Cloudflare 页面验收](../research/country-enriched-release-v1/ui/pages-r1/country-version-ui-qa.json)及[正式站验收](../research/country-enriched-release-v1/ui/production-r3/country-version-ui-qa.json)：同套 10 组检查、各 16 种组合通过，包含错版响应拒绝；三端静态源保持相同字节。

浏览器验收中的故意错版响应是验证前端拒绝行为的合成故障，未改变真实统计。原署名、作者原文、XML／PDF、盲审 Gold 和大结果镜像不进入网站或 Git 仓库。

## 正式服务

正式网站为 `http://192.168.1.229:8899`。进程 `3547541` 使用 `oa-stats-v3`，SHA-256 `a9cec12d1d15a14956ee0699a415f1a400cf80296ba41784959eb986297e48d9`。新服务先加载全部数据，确认等待监听后才关闭旧服务，端口交接至默认补全版约 0.21 秒；四个版本／语料元数据检查通过。旧二进制、启动参数和原结果保留，[交接证据](../research/country-enriched-release-v1/production-promotion.json)记录完整身份与质量字段。

最终[验收索引](../research/country-enriched-release-v1/acceptance.json)绑定六份通过报告及关键源／数据清单 SHA。本次 8898 候选、8798 Pages 兼容预览和 8799 官方 workerd 已在验收后按自有进程身份关闭；正式 8899 与原参考服务不受预览清理影响。Cloudflare 公网项目仍由仓库所有者按既有连接部署，本文的官方运行时验收是本地 workerd，不冒称已经验证某个公网域名。
