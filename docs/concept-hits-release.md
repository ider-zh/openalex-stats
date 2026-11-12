# Concepts 命中计数与 core/all 版本

2026-11-12 更新。计算与界面升级正在执行；本文件中的“待验收”不表示已经部署。

当前统计定义见 [counting-policy.md](counting-policy.md)。四层分别为 Topics Field、Subfield，Concepts Level0、Level1。国家归属为全部 authorships.countries 去重；国家均分与国家全计数独立于 Concepts 阈值选择。命中标签全计数时，国家全计数的论文量和引用量必须为非负整数。

## 两个数据版本

- `core`：排除 XPAC，保留既定黑名单以外的主数据。
- `all`：同一类型政策下包含主数据与 XPAC；先形成统一 Q，再计算 Q 内引用图，包括双方之间的交叉引用。

两个版本均保留 2000 年前的合格被引对象，输出从 2000 年开始。引用年份取施引文献发表年，国家与学科取被引对象。2026 为截至 6 月 26 日的部分年，默认展示至 2025 年。

## 冻结计算版本

- Go executable：`oa-stats-v9`，SHA256 `8be7a2e15906ac7f3c80b1b752395d8b9499ddd871077e63e550d87fefea01a6`。
- 源码归档：`source-v9-compute.tar.gz`，SHA256 `33b93a34feba8a0c2d45a29a6c3254975468745c20a42b51ab49058b886a9d2b`。
- core 输出：`/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1`，32 Go CPU workers、16 readers、288 GiB 预算，复用相同 Q 的旧整数引用图与计数检查点。
- all 输出：`/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1`，14 Go CPU workers、6 readers、320 GiB 预算，独立重建维度、引用图与整数计数检查点。

计算冻结版本不等于最终界面版本。最终服务将单独记录可执行文件 SHA、两个完成清单、独立审计与浏览器验收。

界面候选版本 `oa-stats-v10` 的 SHA256 为 `ac0efd9804294ed05c9f96d28a20b477d46d963e881edd836aff969e56b10fba`，对应归档 `source-v10-service.tar.gz` SHA256 `5b7bad0967a2343def2c3732a820fe9112400d07c94897aa26665cdceb34fc41`。候选冻结不代表已通过真实数据部署验收。

## 展示定义

ECI bump 图按每年全部有效国家的原始 ECI 降序排名，第一名在顶部；并列采用竞赛排名（如 1、1、3）。国家选择只控制显示曲线，不改变排名。悬浮提示同时显示原始分值、名次与有效国家数量。无效模型保留缺口，不能填零或插补排名。也可切回原始分值曲线。

该层的国家 ECI 来自该层全部学科组成的矩阵，不是所选单一学科内部重新计算的 ECI；PCI 为学科复杂度。Java 方法差异与“固定大类内部复杂度”的区别见 [方法 diff](java-method-diff.md)。

默认九国为 CN、US、GB、DE、FR、JP、IN、CA、KR，只是展示默认值。国家选择支持中文、英文、代码搜索。Topics Subfield 使用快照真实 field 父级；Concepts Level1 的快照及对应 API 未提供父级，界面显示父级未提供分组，不推断学科关系。

## 待验收

core 计算已完成：run `4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455`，Q 269,194,661，输出年论文 201,267,475，引用事件 2,363,318,327；复用 2,446 个引用分片及 64 个计数分区。生产校验的原始计数、学科归属次数守恒和全计数整数性均通过。证据见 `research/core-concept-hits-v1/run.json` 与 `completion.json`。

core 候选界面在 8900 端口正式浏览器验收通过 34/34 项，无资产覆盖、页面错误和运行时外部请求。234 个真实 ECI 年度散点与 API 的全局排名逐点一致；原始分值切换通过。2025 年中国 Biology 的国家全计数论文量为 146,958（阈值 0.3）。证据见 `research/browser-core-concept-hits-candidate`。完整独立数据审计与最终 8899 部署尚未完成。

core 独立模型审计已通过 216/216：首年方向、跨年协方差定号、RCA/M、非对称特征方程与双端分值导出均无失败。证据见 `research/core-concept-hits-model-audit-v1/model-audit.json`，SHA256 `79c83ea6eaf2c0d883b12c850c1c7a290def4735f8559b01b3f9bfc65d096d82`。

全源独立审计检查原始 Q、引用事件守恒、去重学科归属次数、全计数整数性、所有默认场景与模型数学/符号/导出。core/all 对应单元格的原始发表量与引用量应单调不减；RCA、份额及 ECI/PCI 不应作这种单调要求。最终浏览器验收同时覆盖两版本、排名不随国家选择变化、年度散点、tooltip 排序、搜索/树控件及下载版本隔离。

后台交付分工已授权：`go_metrics` 完成两版本完整审计；`go_graph` 在 core 完整审计通过后切换 8899 并完成生产浏览器验收；`go_full_corpus` 在 all 计算、完整审计、对应计数单调检查和双库候选验收通过后，将两版本并入 8899 并再次验收。两个服务切换按此顺序协调，避免同时占用端口。8900 为已经通过真实 core 验收的候选界面，不能把它的成功误记为最终双库生产验收。
