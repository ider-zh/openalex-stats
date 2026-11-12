# Cloudflare Pages 部署与完整数据格式

本部署使用 Pages advanced mode：同一份 HTML/CSS/JavaScript、ECharts 和本地 SVG 旗帜用于 Go 服务与 Cloudflare 网站。Go 预先导出统计矩阵、完整分母和模型分值／全局排名；边缘适配层只做参数校验、按条件取值、份额／RCA 的标量计算及 CSV 序列化。

## 从 GitHub 部署

1. 在 Cloudflare 的 Workers & Pages 中新建 Pages 项目，连接 `ider-zh/openalex-stats`。
2. 分支选择 `main`，Framework 选择 None，根目录为仓库根目录。
3. Build command 填 `npm run build`，Build output directory 填 `dist`，Node.js 使用 24 或 22。
4. 保存并部署。构建输出的 `_worker.js` 提供 API；`_routes.json` 仅将 `/api/*` 和 `/download` 交给 Function，其余文件直接作为静态资产服务。

没有运行时研究服务器、LAN 代理、数据库或必需的 R2 binding。源数据与所有展示国家／学科均已包含在本仓库的查询数据中。大型原始结果的下载跳转至本项目公开 GitHub Release；图表加载只请求本站静态分片。

Cloudflare Pages 单个静态资产上限为 25 MiB；当前构建按免费档 20,000 文件上限进行保守校验。`build-pages.mjs` 校验每个分片的字节数与 SHA-256，并在越界时失败。[官方 Pages 限制](https://developers.cloudflare.com/pages/platform/limits/)

Pages Function 使用标准 `ASSETS.fetch` 读取部署内资产及标准 Web 压缩流 API，没有 Node.js server、原生 Go 进程或长期持有的全量数据库。[官方 advanced mode](https://developers.cloudflare.com/pages/functions/advanced-mode/)、[ASSETS API](https://developers.cloudflare.com/pages/functions/api-reference/)

## 本地验证

```sh
npm run build
npm run test:pages
npm run preview
```

预览地址为 `http://127.0.0.1:8788`。这是以 Node 标准 Fetch API 模拟 `ASSETS` binding 的兼容预览；正式部署仍由 Cloudflare 的 Workers runtime 执行 `_worker.js`。HTTP 参数、两语料隔离、CSV、完整分母与两种复杂度方法会与 Go 服务做真实数据 parity 核对；部署不需要把研究服务器暴露到公网。

Cloudflare 的 API 请求数和 CPU 配额取决于用户账户方案；静态文件服务与 Function 请求按平台规则计量。本适配器采用定长 Float64 矩阵直接寻址，避免边缘加载全量模型或解析全学科统计；一次最大 48 曲线 RCA 冷查询最多需要 50 个本地资产子请求。

## 更新查询数据

在能访问完整不可变结果的计算机上构建 Go CLI，然后使用独立输出目录：

```sh
go build -trimpath -o oa-stats ./cmd/oa-stats
./oa-stats export-site \
  --data-core /path/to/core/base \
  --data-all /path/to/all/base \
  --methods-core /path/to/core/methods \
  --methods-all /path/to/all/methods \
  --output /path/to/new-static-export \
  --workers 8 \
  --release-base https://github.com/ider-zh/openalex-stats/releases/download/snapshot-20260626-v1
```

导出前逐项校验原结果 completion 拥有的文件、方法 completion 与来源身份。输出不能位于任何输入结果目录内；已有同名分片只有逐字节一致才可复用，不能静默覆盖不同计算版本。

将完整导出目录内容放入 `cloudflare/data`，保持其中 `export-manifest.json` 与 `data/` 相对结构。不要仅复制默认九国、单一学科或部分年份。运行 `npm run build` 后把源数据导出及代码更新提交到 GitHub，Pages 将按新 commit 自动重建。

## 分片格式

`cloudflare/data/export-manifest.json` 列出所有部署数据的相对路径、字节数和 SHA-256；`cloudflare/data/data/site.json` 包含两语料 catalog、与 Go 一致的 metadata、响应定义、下载映射及稳定的字典顺序。

计数文件路径以语料和基础 run ID 前缀隔离。每个 scenario／layer／discipline 有一个 gzip 压缩矩阵，保留字典所有国家和全部输出年份；国家／全球完整分母独立保存。未观测单元格以二进制零表示。UNKNOWN 归属也保留在数据导出中，但不会被当成有效国家或学科加入 UI 的 RCA/复杂度矩阵。

解压后的 **OAQ1** 格式：

| 偏移 | 内容 |
| --- | --- |
| 0–3 | ASCII magic `OAQ1` |
| 4–5 | little-endian uint16：起始年 |
| 6–7 | little-endian uint16：年份数 |
| 8–9 | little-endian uint16：国家槽位数，含最后一个 UNKNOWN 槽位 |
| 10–15 | 保留字节，当前为零 |
| 16 起 | 每年一对 little-endian IEEE-754 Float64：publications、citations 的完整学科／全球总量 |
| 随后 | 国家优先、年份次优先的 Float64 publication/citation 对 |

所有原始 Float64 位值直接保留，不按展示精度四舍五入。计数、份额及 RCA 不受学科／国家选择集合改变分母。模型分片按方法／层级／输入指标保存预计算的 `[原分值, 状态, 全局名次, 有效实体总数]`，Function 不重新计算模型或用所选曲线重新排名。

## 原始结果和方法 sidecar

完整原始产物超过 GitHub 普通 Git 单文件限制，保存为 Release assets。每个资产小于 GitHub 的 2 GiB 上限；`results/index.json` 记录原文件 SHA、大小、语料和 base/methods 分组。[官方 Release 限制](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases)

```sh
node scripts/download-results.mjs --corpus core
node scripts/download-results.mjs --corpus all
```

下载器验证每个文件，已存在的正确文件可以复用，冲突文件不会被无声覆盖。方法 sidecar 必须与相应基础 run/completion 精确匹配，不能把 Core 的反射法分值用于 All，或用旧分摊版本的模型配新命中计数。

## 本次发布验证

完整查询导出包含 7,133 个数据文件、193,230,749 字节；网站构建包含 7,397 个文件、约 196 MB，最大文件 2,698,129 字节。完整原始下载另有 44 个 Release 资产，共 2,761,138,916 字节。

独立 Go 核验程序直接读取原 Parquet 和模型 JSON，逐位检查两个语料合计 29,980,020 个输入单元格、全部计数分母、864 个模型的 339,768 个分值与全局名次，以及全部导出文件 SHA，差异为零。结果见 `research/cloudflare-delivery/export-verification.json`。

Pages 适配器与原 Go 服务进行了 962 个真实请求比较，覆盖两语料、四层、六场景、两方法和完整年份，包括 120,636 个点值、24 个 CSV、12 个小型下载及 12 个错误请求。元数据中的国家和标签列表按 ID 比较语义集合：同名学科在两个 Go 进程中可能因 map 遍历顺序而排列不同。请求返回的曲线顺序、点值、状态、全局排名和口径字段仍严格比较；CSV 数字按 Float64 数值比较，文本与行顺序严格比较。证据见 `research/cloudflare-delivery/pages-parity.json`。

本地浏览器验收通过同源 HTTP 访问实际构建输出，检查桌面和手机上的国旗、多学科、字体、tooltip、方法切换及曲线数据。大结果下载通过 Release 导航完成；本地预览不模拟 Cloudflare 账户计费或公网部署，其结果不能代替部署后的域名连通性检查。
