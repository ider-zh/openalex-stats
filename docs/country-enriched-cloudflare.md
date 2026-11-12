# Cloudflare 国家数据版本

网站提供两个独立的国家数据版本，公开参数为 `country_version=original|enriched`。双版本导出默认选择 `enriched`；省略参数不会回落到原始国家字段。显式 `original` 链接持续访问原数据。只有原始导出的旧网站保持原版默认，并拒绝请求不存在的增强版。

国家版本、语料 `corpus=core|all` 和复杂度 `method` 相互独立。每种国家版本都包含各自完整的 Core／All 计数、全量分母、RCA、谱方法和小倪方法论预计算模型；边缘函数不重算模型或根据选中的国家重排全球名次。

## 数据合并

分别用 Go `export-site` 导出原始与增强数据至两个独立目录，再运行：

```sh
node scripts/combine-country-exports.mjs --original /path/to/original-export --enriched /path/to/enriched-export --output /path/to/new-combined-export
```

输出目录必须不存在、其父目录必须存在，且不能位于两个输入目录内。脚本先验证全部源文件的字节数和 SHA-256、语料和完成身份、年份与文献集合，再复制分片原字节。相同路径若内容不同则拒绝合并；输入目录不修改。合并后用新 `export-manifest.json` 重建 `cloudflare/data`，再按常规执行 `npm run build`。

合并没有应用额外国家推断，没有调分数、方向或排名。源数据的国家变化以 r3 已审计产物为准。大文件下载使用各自导出中记录的 GitHub Release URL，发布前须确保增强版对应资产已经上传并通过大小／SHA 核验。

## 控制格式与接口

- `site.country_versions` 是 `{original:{core,all},enriched:{core,all}}` 控制对象。每个控制保留自己的 run ID、完成 SHA 和独立分片前缀，缓存按实际资产路径隔离。
- `site.catalog.country_versions` 是给界面的版本数组。项为 `{id,display_name,version}`；默认字段为 `default_country_version:"enriched"`。
- `site.datasets` 保留默认增强版控制，供原单版本格式读取工具查看；正式 Worker 路由以 `site.country_versions` 为准。
- 元数据回显 `country_version` 和 `country_version_info`，序列 JSON 在 `query.country_version` 回显选择，CSV 末尾增加 `country_version` 列。
- API、CSV 和下载均携带 `X-OpenAlex-Country-Version`；未知、空、重复或未部署的版本参数返回 400。
- 原始版标识 `snapshot-20260626-original`；增强版标识 `country-enriched-v3-r3`。这是国家字段版本，不是文献类型、年份、语料或复杂度方法切换。

方法说明及覆盖率以[国家补全最终 review](country-enrichment-v3-review.md)为准：本批仅为原国家为空的论文补入严格署名文本国家，未系统修复原有非空国家误配，外部元数据本批实际准入 0。冻结留出的 255／256 是文本候选源证据核验结果，不是全部原数据国家准确率。

## 验证

```sh
node --test cloudflare/worker.test.mjs scripts/combine-country-exports.test.mjs
```

Worker 测试保留原查询边界，并以四组不同数据验证两国家版本 × 两语料计数、模型、全球排名、CSV、下载、HEAD 与参数错误隔离。合并测试独立检查原输入未变、分片逐字节保留、清单全部 SHA、自覆盖和损坏／冲突拒绝。真实数据的全量导出及 Go／Pages parity 另由本次发布验收记录确认，合成测试不代替真实统计验收。

### 2026-11-13 真实数据验收

`scripts/country-version-parity.mjs` 对真实 Pages 构建预览与 229 Go 候选服务完成 1,984 个序列 JSON 查询、667,008 个点值、56 个 CSV、4 套元数据、48 个小产物字节／SHA、32 个大产物 HEAD、14 个参数错误、8 个 HEAD 及 4 个缺参默认增强版检查，全部一致。覆盖四数据集、四层、六场景、两方法、两种模型输入与全 248 国 ECI；分值、状态、全球名次／有效总数及 `source_run_id`／国家版本逐项核对。

32 个大产物还通过 GitHub Release 最终 URL HEAD 的实际大小验证。基线 `completion.json` 不在公开下载白名单，因此从导出中按已记录 SHA 读取，再以 Go 实际公开 `run.json` 字节／SHA 绑定来源；方法 completion 使用实际公开下载。大文件 SHA 与对应 completion 和所选静态下载控制一致，不将 HEAD 大小比较冒充完整大文件重新下载哈希。完整上传资产 SHA 核验由发布审计另报。

672 组 original／enriched 配对查询观察到真实计数或排名变化，确认切换没有只改变标签。记录见 [pages-parity.json](../research/country-enriched-release-v1/parity/pages-parity.json)。此轮验证本地 Pages 适配预览与 Go 候选服务，不能代替最终 Cloudflare 公网域名部署后的连通性确认。

随后对同一 `dist` 使用官方 Wrangler 4.160.1／workerd 本地运行时重放整套真实验收，以上 1,984 查询、667,008 点值、56 CSV、48 小产物、32 大产物及其最终 GitHub HEAD 等检查再次全部通过，未出现 ASSETS 或运行时内存错误。记录见 [workerd-parity.json](../research/country-enriched-release-v1/parity/workerd-parity.json)。[进程资源观察](../research/country-enriched-release-v1/parity/workerd-resources.json)记录 Windows 整进程常驻集／峰值及累计 CPU，不是单 isolate 堆或生产请求 CPU 用量，不能据此推断 Cloudflare 账户方案配额。
