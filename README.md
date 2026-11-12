# OpenAlex statistics

按国家、年份和学科统计论文量、当年被引、份额、RCA 与知识复杂度，包含 **Core 主数据**和 **Core + XPAC 全数据**两个经过独立审计的版本。

本仓库包含 Go 计算与导出源码、研究口径和验证文档、完整网站查询数据、Cloudflare Pages 网站，以及原始统计产物的 GitHub Release 下载清单。网站部署后直接查询本次发布的数据，不依赖研究服务器或私网 API。

## 数据版本

快照日期 **2026-06-26**，原始 510,372,821 Works。文献集合保留合格的 2000 年前被引对象；输出年份从 2000 年开始，默认展示 2000–2025，2026 标记为部分年度。

| 版本 | 合格文献集合（全部年份） | 输出年发表文献 | 有效引用边 |
| --- | ---: | ---: | ---: |
| Core | 269,194,661 | 201,267,475 | 2,363,318,327 |
| Core + XPAC | 404,079,112 | 285,365,821 | 2,385,146,215 |

四个层级独立统计：**Topics Field、Subfield；Concepts Level 0、Level 1**。Topics 采用 primary topic；Concepts 同层标签先去重，命中每个标签各计一篇。六个场景组合国家均分／全计数与 Concepts score 阈值 0、0.3、0.5。国家取本篇论文全部署名的 `authorships.countries` 去重集合；国家全计数下论文量和被引量为整数，国家均分保留合作分摊的小数。

对 A→B 的引用，两个端点均须在该版本的文献集合内；年份归于 A 的发表年，国家和学科归于 B。引用去重，排除同 Work 自环，保留作者自引。全数据的引用图包含 Core 与 XPAC 之间的交叉引用，不是两个独立统计的简单相加。

## 网站与方法

- 同层学科多选，一般统计绘制“国家 × 学科”的曲线，最多 48 条并明确提示超限。
- 国家搜索、真实 Topics 学科树、国旗 SVG、大字号、年度散点、当年值降序 tooltip 和 CSV 导出。
- ECI 是当前层全部学科结构的国家复杂度，只按国家绘制；PCI 是各学科的全球值，只按学科绘制。
- 复杂度提供全局 Bump 排名和原分数曲线。排名基于当年全部有效实体，改变所选国家／学科不会重新排名。
- 方法 switch 保留默认 `spectral-v1`，另提供 `java-reflections-v1` 修正版。两方法共享同一 X、RCA、二值网络、标签和计数政策；反射法采用国家 16 轮、学科 17 轮、总体 z-score，并明确记录定向、异常状态及敏感性验证。

方法解释与限制见 [计数政策](docs/counting-policy.md)、[Java 方法 diff](docs/java-method-diff.md)、[Java 反射法修正版](docs/java-reflections-method.md)及 [Core/All 发布说明](docs/concept-hits-release.md)。ECI/PCI 是年度相对结构指标，不是知识总量。

## 在 Cloudflare Pages 部署

在 Cloudflare 新建 Pages 项目并连接此 GitHub 仓库：

| 设置 | 值 |
| --- | --- |
| Framework preset | None |
| Root directory | 仓库根目录 |
| Build command | `npm run build` |
| Build output directory | `dist` |
| Node.js | 24（至少 22） |

不需要数据库、R2、研究服务器地址或应用 API key。Pages 自动加载构建输出中的 `_worker.js`；它验证查询条件并读取预计算分片，不在边缘重算 ECI。代码与 gzip Float64 数据分片已在仓库中，构建只校验和复制文件。

完整步骤、数据格式和更新方式见 [Cloudflare 部署文档](docs/cloudflare-deploy.md)。本地预览：

```sh
npm run build
npm run preview
# http://127.0.0.1:8788
```

## 原始结果下载

[GitHub Release：snapshot-20260626-v1](https://github.com/ider-zh/openalex-stats/releases/tag/snapshot-20260626-v1) 保存两版本完整 Parquet、模型 JSON、清单、诊断和独立方法 sidecar。文件 SHA-256、大小及下载地址见 [results/index.json](results/index.json)。

已发布 44 个原始结果资产，共 2,761,138,916 字节，逐文件通过 GitHub 服务端 SHA-256 和公开下载验证。采用 Release 资产而不是 Git LFS；不把 724 GB 原始 Works 快照和中间引用图放入 Git。网站的全部可查询数据另以约 193 MB 的分片保存在 `cloudflare/data`，覆盖两个语料、四层学科、六场景、全部国家及输出年份。

```sh
node scripts/download-results.mjs --corpus core
node scripts/download-results.mjs --corpus all
```

下载器流式写入 `results/full` 并验证 SHA-256；原始结果不会因此进入 Git。

## Go 计算与再导出

需要 Go 1.26+。统计服务、聚合和发布数据导出均为 Go；Cloudflare 的轻量 HTTP 适配与构建脚本使用平台原生 JavaScript。

```sh
go test ./...
go vet ./...
go build -trimpath -o oa-stats ./cmd/oa-stats
node --test cloudflare/worker.test.mjs
node scripts/bump-chart-test.mjs
```

本地完整 Go 服务可直接读取下载的不可变结果和方法 sidecar：

```sh
./oa-stats serve \
  --data-core results/full/core/base \
  --data-all results/full/all/base \
  --methods-core results/full/core/methods \
  --methods-all results/full/all/methods \
  --listen 127.0.0.1:8899
```

静态查询数据的导出命令和格式见部署文档。原始快照计算使用 `prepare`、`compute`、`audit`；输入和范围、构建身份、各阶段资源与 SHA 均由不可变 manifest 记录。历史 v7/v8 分摊版本及其复现命令见 [历史交付记录](docs/historical-v7-delivery.md)，不可与当前 Concepts 命中计数结果混用。

## 国家覆盖率与研究状态

网站默认国家补全版，可切回原始版；该选择与 Core／含 XPAC 全数据、分类和复杂度方法独立。2000—2025 完整年论文国家覆盖率：Core **49.3127%→50.3009%**，Core+XPAC **36.8614%→37.5577%**。页面顶部覆盖率包含 2026 部分年，补全后分别约49.64%、37.53%。UNKNOWN 仍保留在基础统计中，不进入 ECI/RCA 模型。详见[双版本发布说明](docs/country-enriched-release.md)。

本次补全仅采用已通过冻结留出核验的严格署名文本路线，为原国家缺失的论文补入国家；已有非空国家误配尚未全面修正。Crossref／PubMed／开放全文候选本批未准入。原始版结果保留，原作者署名、盲审证据和原全文不发布。

第三方浏览器资产的版本、许可和来源见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

本次 [交付验收记录](research/cloudflare-delivery/acceptance.json) 包含全量数据核验、与正式 Go 服务的 API 对照、官方 Cloudflare 本地运行时及桌面／手机浏览器验证。Cloudflare 公网项目由仓库所有者连接并部署。

全球 ECI Bump 默认展示全部 248 个国家／地区，使用 SVG 和至少 26px／名次的自适应长图，按终点标签所需空间继续加高。普通数量指标仍限制 12 国／48 条组合曲线，其国家选择与 ECI 独立保存。详见 [交互说明](docs/explorer-controls.md)。

首次成功打开图表后显示六步使用导引；完成、跳过或关闭后满七天再次提示，页首可随时重看。规则与验证见 [使用导引](docs/onboarding.md)。

2026-11-13 研究结果已归档：[小国家／地区复杂度高排名审查](docs/small-country-complexity-review.md)、[国家补全研究 review](docs/country-enrichment-v3-review.md)及[可复核数据与工具](results/research-review-20261113/README.md)。用户 review 后，国家补全 r3 已另作为网站可选版本发布并设默认，原始版保持可选；小倪方法论的重命名及默认方法保持，见[此前发布验证](docs/xiaoni-default-release.md)。

原 Windows Java 项目中的国家名单、低多样性排名筛选、规模门槛备选与入口调用核查，见[小倪 Java 长尾处理审查](docs/xiaoni-java-longtail-review.md)。这些旧代码机制未自动加入当前网站。
