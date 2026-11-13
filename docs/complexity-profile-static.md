# 数据支撑方案：静态模型与边缘服务

数据支撑筛选与国家版本、Core／All、复杂度方法独立。18个已计算方案加未筛选 baseline，默认 `p1000-c1000-x5-d3-v1`；它要求国家当年全计论文≥1000，引用模型还要求当年被引事件≥1000，单格国家均分赋值≥5，最终优势学科数≥3。完整参数是公开离散网格，不接受任意值后在边缘临时求解。

模型先筛国家规模、将弱格置零，再同步永久移除优势学科数不足的行，反复重算全部 RCA 边际至固定点。保留国家合作份额不重新分配。谱与小倪 ECI／PCI共用最终网络；基础论文数、引用、share及基础RCA图不变，回显 `profile_applies=false`。这限制数据支撑不足的对象，不替代原国家误配修正或国家能力有效性验证。

## 从完成的 Go bundle 导出

`scripts/complexity-profiles` 生成四个不可变 bundle，先通过原生模型、排名、Parquet与方向审计；然后运行：

```sh
node scripts/export-complexity-profiles.mjs \
  --base /path/to/immutable-country-static-export \
  --bundle /path/to/bundles/original-core \
  --bundle /path/to/bundles/original-all \
  --bundle /path/to/bundles/enriched-core \
  --bundle /path/to/bundles/enriched-all \
  --output /path/to/new-profile-static-export
```

新目录必须不存在、父目录须存在，且与所有输入分离。工具验证原静态清单、各源artifact SHA和国家／语料 run及completion绑定；按ID映射国家／学科顺序，直接读取Go原生scores和竞争排名，不重新求解、改方向或生成替代名次。baseline直接保留现有科学分片原字节。source bundle必须声明 `baseline-pooled-common-year-sign-v1`，旧独立方向版本仅作研究证据，不能混入首发。

## 路径、控制与下载

- `catalog.complexity_profiles`／metadata提供同一19项ProfileInfo及`default_complexity_profile`；每项包含阈值、engine、baseline和direction_policy。
- 查询使用`complexity_profile`，回显在query；响应另含`complexity_profile`对象、`profile_applies`、`profile_run_id`和`model_context`。
- 每个非baseline profile只有一件外置manifest，包含四数据集对应的模型路径与下载映射。所有路径带base run及profile_run标识，manifest名称含内容SHA，避免版本间缓存混用。
- 每shard含某数据集／层／输入／方法的27年ECI和PCI，点保留全局竞争排名；筛掉的点无值无名次，并提供原因和全计论文／引用／最终优势数。选中的曲线不会改变这些名次。
- `model_context`提供年度有效国家／学科、保留赋值比例、闭包轮数与X／M哈希。baseline年度context为空，保留原分数和方向；非复杂度查询不额外读取profile manifest。
- JSON下载以gzip静态存储，descriptor bytes/SHA是原解压字节身份，部署清单则校验存储gzip本身。Worker解压原stream而不重新序列化，HEAD回原大小；Parquet直接返回原字节。
- 非baseline的`eci.json`、两种score Parquet、当前profile来源／诊断／方向下载只来自对应profile，未重算的12/20轮敏感性不会回退基础模型。基础statistics、cube、RCA、run、coverage下载仍是原计数数据。

方向策略先保留既定年度链，再以2000—2025共同国家逐年中心化的协方差合计定一个整27年的factor，同时乘ECI与PCI；不逐年／不按指定国家调方向。策略及弱相关记录包含在`profile-direction.json`中。baseline标记`published-baseline-v1`。

## 限额与验收

原国家版本部署约14,531文件。18个profile新增1152个模型shard，再加轻量下载、来源和方向；布局仍低于20,000文件，单个存储资产限25MiB。最终以真实export-manifest和`npm run build`计数为准。模板和metadata不按profile复制，模型缓存16项，另只保留一份site control；普通48曲线RCA保持原50子请求上界。

```sh
node --test cloudflare/worker.test.mjs scripts/export-complexity-profiles.test.mjs
PAGES_URL=http://127.0.0.1:8798 GO_URL=http://192.168.1.229:8898 node scripts/complexity-profile-parity.mjs
```

真实parity覆盖全部profile、四数据集、两方法、四层、两种模型输入，比较原生值、状态、全球名次／总数、来源与支撑诊断；baseline／默认方案另验全国家请求。另核基础统计跨profile不变、CSV、原始字节下载、HEAD及无效参数。正式本地workerd重放与最终服务检查另记录，不能由单元合成fixture替代。

## Final v2 acceptance

The final build contains 16,349 files / 870,119,276 bytes; the largest file is 4,105,647 bytes. Its 16,083 data assets total 867,252,733 bytes. Both Pages preview and official workerd passed 3,016 JSON queries (1,255,176 points), 152 CSV comparisons, 888 download HEAD checks and 40 complete download byte/SHA checks. The suites also verified 432 unchanged basic-metric cases and 608 method-independent support/mask cases. A freshly restarted workerd passed all 12 simultaneous cold requests; real-browser QA passed all 11 groups.

Only completed parsed objects enter the shared cache; pending ASSETS/decompression I/O belongs to its request. HEAD must advertise the exact raw entity length. GET may stream chunked without Content-Length; any advertised length must match, and actual body length plus SHA always match exactly. All 40 official workerd downloads used valid chunked streams with identical bodies. Final reports are under `research/longtail-patch-v2/parity/`; build identities and report hashes are recorded in `research/longtail-patch-v2/runtime-acceptance.json`.
