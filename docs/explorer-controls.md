# 多学科对照与知识复杂度方法

默认展示 9 个国家和 1 个学科。同一分类规范、同一层级内可以搜索并多选学科；已选标签支持逐项移除、清空、复位和选中搜索结果。切换 Core／含 XPAC 数据会保留有效筛选，刷新或分享 URL 也可恢复选择。

| 指标 | 曲线数量 | 学科选择的含义 |
|---|---:|---|
| 论文量、当年被引、全球学科份额、RCA | 国家数 × 学科数 | 每个国家／学科组合各一条 |
| 国家 ECI | 国家数 | 使用该层全部学科，学科筛选停用，不重复复制国家曲线 |
| 学科 PCI | 学科数 | 每个学科一个全球值，国家筛选不改变曲线 |

“数量与引用”“知识复杂度”“比较优势”分组中的按钮均可直接点击。ECI 和 PCI 放在知识复杂度组；RCA 明确是比较优势指标。论文量、当年被引、份额和 RCA 最多选择 12 个国家、最多生成 48 条曲线；例如 9 国 × 3 学科为 27 条。超限会明确提示并拒绝操作，不截断结果。

国家使用一致颜色；同一国家的不同学科使用不同线型和标记组合。图例可翻页，提示框按悬浮年份的值降序，长提示框和终年表可以滚动查看全部曲线。国家选项、已选标签、图例、提示框和表格采用本地 SVG 国旗；GLOBAL 使用中性图标。图标来自固定版本 flag-icons 7.6.0，MIT 许可与来源清单在 `internal/web/static/flags/`，运行时无需访问 CDN。

## 方法选择

- `spectral-v1`：现有谱 ECI，默认方法。
- `java-reflections-v1`：Java 思路的修正版，国家 16 轮、学科 17 轮，使用总体 z 标准化。

两方法复用相同的已审计国家×学科矩阵、RCA、二值网络和有效标签，各自按已定义规则定向。有限次迭代可能与谱方法产生显著不同的名次，不能承诺近似同榜。方法区提供固定的轮数敏感性报告入口，详细数学定义见 [java-reflections-method.md](java-reflections-method.md)。

方法选择仅影响 ECI／PCI。论文量、被引、份额及 RCA 保持原统计口径；多选学科也不会把 RCA 的全部学科分母缩成所选子集。Bump 排名来自当年完整模型的所有有效国家或学科，选择曲线不会重新排名；原始分值可以一键切换查看。

## API 与导出

旧单学科参数 `discipline=<ID>` 保持兼容。新参数 `disciplines=<ID1>,<ID2>` 接受同层 ID 列表并去重；新旧参数不能同时出现。未知、跨层、重复参数和超限请求返回明确的 400 错误。

`method` 缺省为 `spectral-v1`。`/api/meta` 的 `methods` 列出该数据版本实际加载的方法、轮数及源运行身份；未配置或校验失败的方法不会出现在界面中。

每条 series 显式包含 `country`、`country_name`、`discipline`、`discipline_name`。单学科保留旧 country series ID；多学科组合使用稳定的 `country::discipline` ID，PCI 的 country 为 `GLOBAL`。CSV 保留原有列的位置，并增加组合 ID、标签及 `method`、国家／学科迭代轮数、`source_run_id`、`method_applies`。

方法下载使用白名单：`eci.json`、两个 score Parquet、`model_diagnostics.json` 可以带 `method=java-reflections-v1` 选择对应旁路产物。另有 `method-manifest.json`、`method-completion.json`、`method-audit.json`、`method-sensitivity.json`。Java 方法的下载文件名带方法前缀，文件字节与原始完成清单中的 SHA 保持一致。计数、字典、覆盖率和原始运行文件继续来自基线数据。

## 服务配置与验收

`serve --data-core <core> --data-all <all> --methods-core <core-sidecar> --methods-all <all-sidecar>` 加载独立的两方法旁路。方法目录必须位于基线结果以外；启动时校验完成清单、全部文件 SHA、源 run／completion 身份，以及每个模型的完整共享网络和标签。请求只能选择已发布的方法 ID，不能传入文件路径。

多学科／国旗版本证据在 `research/ui-multi-discipline-v12/final-acceptance.json`。两方法版额外用 HTTP 原始模型文件逐点核对 API 的分值和全球排名，检查计数不随方法变化、方法特定下载、CSV 溯源、URL／语料切换及桌面与手机布局。独立数值计算审计属于旁路方法结果的验收，前端不会重新计算算法。

## 全球 ECI 长图与国家选择

首次进入全球 ECI Bump 默认显示元数据中全部国家／地区（当前 248 个），显式 URL 国家子集则按该子集恢复。ECI 分数折线也允许完整国家集合；数量指标继续保留 12 国／48 曲线限制，并独立保存原选择，切回时不会截断全部国家。

Bump 使用 SVG，按完整年度模型的最大排名提供至少每名次 26px 的空间，再根据终年有效国家名称所需的总标签高度自动加高。手机长名称会分行，所有有效终点使用国旗和国家名；无效国家保留在选择、悬浮提示和终年表中，不伪造分数。页面纵向滚动，全球名次不随筛选重排；悬浮卡片限制在当前屏幕内，248 行提示可滚动。
