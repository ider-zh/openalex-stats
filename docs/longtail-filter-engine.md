# 长尾可靠性筛选：离线计算引擎

实现版本：`country-scale-cell-zero-monotone-diversity-v1`；产物格式：`complexity-filter-bundle-v1`；首发方向约定：`baseline-pooled-common-year-sign-v1`（v2 数据目录）。本引擎在复杂度计算前筛选并重算内部 RCA、ECI、PCI，保留未筛选原版对照。它不改变原论文数、当年被引、份额或数量场景中显示的原 RCA。v1 未作跨 profile 参考定向的产物保留为研究证据，不覆写。

该方案借鉴旧 Java 的国家总量、单格量与 `kc0` 思路，但明确修正了旧实现混用分母、只筛榜重编号、筛辅助文件未改 M 等问题。没有使用国家白名单，没有针对国家排名调整正负方向。预设是工程起点，不代表已经验证的通用国家科研能力标准。

## 输入与支持量

每个国家数据版本、语料、年、学科层、输入指标独立构造矩阵 `X`。四组源为原始／国家覆盖补充 × Core／All；每组 2000—2026 共 27 年、Topics Field／Subfield、Concepts 0／1、论文／当年被引，共 216 个矩阵。每个筛选配置、每种方法因此有四组共 864 个年度模型。

- `X` 直接来自源 `statistics.parquet` 的 `fractional-concept-whole-0.3`，只含已知国家、已知学科；保持原字典标签顺序及未观测零格。
- 年度国家论文支持量取 `whole-concept-whole-0.3` 的 Topics primary Field，**包含 UNKNOWN 学科**后求和。每篇在同一国家记一次，不用多标签 Concepts 的行和冒充去重论文数。
- 年度引用支持量取同一个 whole-country primary Field 的当年被引事件。它**不是不同被引目标论文数**；一个目标被引很多次仍可能通过事件门槛，因此本方案不能完整排除单篇集中风险。
- 2026 仍为部分年；筛选阈值不会把它变成完整年度。

输入接口见 `internal/complexityfilter.Input`：固定 `eci.Matrix`、`Metric`、与国家顺序一致的 `WholePublications`／`WholeCitations`。原统计中的 UNKNOWN 国家仍保留供数量和覆盖统计使用，但不进入复杂度矩阵。

## 冻结的有限配置

`baseline-v1` 为原已发布未筛选模型。其实际输出直接裁剪拷贝原谱／小倪模型，保持原分值、有效集合、诊断、方向；不以本次重算的近似值替代对照。门槛全 0 的重算仅产生筛选诊断，并核对原 M 完全相同。

另有 18 项配置：

| 参数 | 可选值／规则 |
|---|---|
| `country_min_publications` | 100、1,000、5,000 |
| `country_min_citation_events` | 1,000，仅引用输入应用；论文输入不应用此门槛 |
| `cell_min_support` | 0、5、20；单位为原 X 的国家均分、学科命中赋值 |
| `min_diversity` | 1、3；单位为重新计算后 RCA≥1 的优势学科个数 |

配置 ID 为 `p{P}-c1000-x{X}-d{D}-v1`，默认 `p1000-c1000-x5-d3-v1`。所有最低量使用 **大于或等于**；等于门槛者保留。优势学科数不是论文样本量，且四个分类层级的粒度不同，界面必须披露参数，不能称同一个门槛在四层均已获得外部效度验证。

## 精确计算顺序

1. 验证全部原 X 有限、非负及维度正确；不允许坏值躲在将被过滤的行内。
2. 国家全计论文数小于 `country_min_publications` 的整行永久置 0；引用输入另将全计被引事件小于 `country_min_citation_events` 的整行永久置 0。两条件为且关系准入；对应失败原因独立保留。
3. 对余下每个原 X 单格，若 `X < cell_min_support`，将其置 0。**这是零化 X 后重算分母，区别于仅在旧 RCA／旧 M 上删除弱边。** 保留格不重新配权；删掉一个国家后，原国际合作份额不转分给其余国家。
4. 从当前 X 重新计算国家行和、学科列和、总和，内部 RCA 为 `(Xij / row_i) / (col_j / total)`。完全沿用生产 `eci.NormalizeRCA` 的 1e−12 临界容差，以 RCA≥1 建 M。
5. 计算各当前正量国家在新 M 上的 `kc0`。将所有 `kc0 < min_diversity` 的正量国家**同步、永久整行置 0**，再返回步骤 4。已删除者不重新进入，不把这一步当单次旧 M 后筛。
6. 没有新增删除时达到单调固定点。每个非终止轮至少删一国，所以至多 `国家数+1` 轮；空行不参与模型。
7. 对最终 X 调用原生产 `eci.Compute` 一次；有限反射使用该结果的 `eci.ReflectionsFromPrepared`、原 16／17 轮和已修复的稳定总体 z-score。两方法共享最终 RCA、M 和有效标签空间。

迭代中的度数计算只做轻量 RCA，算术循环顺序和边界容差与生产实现相同；最后还逐国核对生产 M 的度数再接受结果，避免每个暂态轮都作昂贵特征分解。没有修改 `internal/eci` 的生产代码。

零度学科不参加最后的谱／反射求分；RCA 的列是否正量、相应分母仍按最终真实 X 决定，不因展示是否有优势边而另换总和。断连、单行单列、重根或反射信号不足继续使用生产状态和诊断，不补造 0 分或全局名次。

## 年度方向和排名

每个 profile、每种方法、每个层、每个输入指标先独立遵循既定年度方向：首个有效年使用生产默认正多样性约定；后续使用原 `pipeline.AlignModelYears`，以共同国家连续性同时翻转 ECI／PCI。

随后采用显式跨 profile 参考定向，避免同一结构轴因弱首年锚点而在切换筛选参数时整条镜像。对同一数据版本、同一方法的已发布 baseline，固定取全部 2000—2025 完整年：两模型有效且共同国家至少 2 个即纳入；按该年共同国家分别中心化，再累计所有共同国家－年对的 covariance／两侧 variance 总和。国家 ID 和年份均排序，均值及三个累计量使用 Kahan 补偿求和。某一年一侧方差为 0 也不删除该年，仍累计另一侧方差。

从 pooled covariance 符号选择**一个** factor，同时施于该 profile／方法／层／输入的全部 27 年 ECI 与 PCI，包括未用于选 factor 的 2026。不按年份、国家身份、目标名次或方法之间重选方向。`abs(pooled rho)<0.2` 标 `weak_reference=true`；没有共同有效年或 pooled 总方差为 0 时 factor=1、标 `unavailable`。raw／aligned pooled rho、因子、固定年份范围、共同对数／年份、来源 baseline SHA 均保存。baseline 实际输出仍逐值原样复制，不重新定向。

这个约定固定跨配置比较的坐标方向，不证明轴正端具有国家科研能力更强的含义，也不消除真实结构变化。各方法仍分别参照各自已发布 baseline，不强制两种方法同号。另保存定向后的逐年 signed／absolute correlation；v1 原方向和逐年对照完整保留，可结合 factor 复核定向的影响。全局竞争排名按 profile 的全部有效国家／学科生成，保留精确并列 `1+#严格更高分者`。不因用户所选曲线改变分母或名次。无效模型／无效标签的分数有效标志为 false，rank 为 null，不伪装排名 0。

## 离线 CLI 和来源绑定

```sh
go build -trimpath -o complexity-profiles ./scripts/complexity-profiles
GOMAXPROCS=12 ./complexity-profiles \
  --data /path/to/immutable/base \
  --methods /path/to/matching/published/methods \
  --corpus core \
  --dataset-version original \
  --output /existing/parent/new-profile-bundle \
  --workers 12
```

默认使用上述 19 项完整配置；`--profiles` 可指定离线冻结 JSON 数组作研发验证，运行时网站只接受已计算 manifest 的有限 ID，不在线接受任意阈值。输出目录必须全新且位于所有不可变输入之外。源 completion 逐项拥有的实际输入 bytes／SHA 先核验、完成前复核；方法还必须绑定同一 base run／completion。仅单次列投影流读统计，同时抽取主矩阵和 whole 支持量，不再扫描原 Work／引用边。

每个 `profile_run_id` 为 SHA-256，输入是 `encoding/json.Marshal` 对嵌套 map 编码的 UTF-8、无尾换行字节。所有 map 键由 Go 标准库按字典序排序；数值使用标准 JSON 数字、字符串使用标准库转义。键包括：bundle version、algorithm、**direction_policy**、source run ID、source／method completion SHA、statistics／dictionary／scenarios SHA、原两法模型 SHA、engine binary SHA、corpus、dataset version，以及 profile 的六个完整定义字段。二进制身份来自实际 `os.Executable`；不同构建链可产生不同 ID，需要新来源记录，不能冒称同一冻结执行。

最终 X／M 另有跨语言可重建 SHA：头分别为 ASCII `OA-COMPLEXITY-MATRIX-1\n`／`OA-COMPLEXITY-NETWORK-1\n`，随后 uint64 little-endian 国家数、学科数；全部国家、学科标签依次以 uint64-LE UTF-8 字节长度加字节写入；MATRIX 再写 row-major IEEE754 Float64 位值，NETWORK 写每格单字节 0／1。全量标签包括被排除者，防止重编号子集冒充相同网络。

## Bundle／下载格式

根 `manifest.json` 包含 `version`、`direction_policy`、`source_run_id`、`source_completion_sha256`、`corpus`、`dataset_version`／`country_version`、`year_start`、`year_end`、`scenario_id`、`default_profile_id`、源码输入 SHA、实际 engine binary SHA、原模型 SHA 及 `profiles`。每个 profile entry 包含：

- `profile`、`profile_run_id`、`model_source`。
- `models`：两方法的轻量 `[]pipeline.ModelResult` JSON。原标签顺序、active indices、ECI／PCI、状态／谱诊断保留；大 RCA、Advantage、Components 数组省略，X／M SHA 在诊断中。
- `ranks`：两方法 JSON 数组，逐年 `country_ranks`／`discipline_ranks` 对齐模型 active 数组，另有对应 universe。无效模型为空 ranks、universe 0。
- `diagnostics`：逐年 `{year,scheme,level,metric,diagnostics}`，包含 profile、原／各阶段／最终赋值量、保留比、逐轮删除、逐国 whole P／whole C／basis events／最后 kc0／原因及 X／M hash。
- `baseline_comparisons`：定向后的共同国家逐年 signed／absolute correlation，不逐年另选方向。
- `direction_policy`／`alignment`：`direction-alignment.json` 记录逐链定向；baseline 为空 chains 并注明 `copied_publication`。
- `downloads`：两方法各自 `eci.json`、`country_eci.parquet`、`discipline_pci.parquet`；shared 为 `profile-diagnostics.json`、`model_diagnostics.json`、`profile-direction.json`、`profile-manifest.json`。最后者实际指向不含自身哈希的 `profile-sources.json`，避免自哈希循环。

所有 Artifact 均为 `{path,bytes,sha256}`，path 相对 bundle 根。分值 Parquet 保留 production ScoreRow 字段，另有 `profile_id`、`profile_run_id`、`source_run_id`、`method`、可空 `global_rank`、`global_universe`，使下载无法混淆数据源、方法和筛选配置。没有为新 profile 挂载旧 12／20 轮敏感性文件。

保留赋值比例的分母是原 **已知国家×已知学科的 X 赋值总量**。Concepts 多标签下不等于去重论文保留率，也不等于准确率或科研质量；极小支持与原国家误配、分类覆盖、单篇集中、轴的能力含义仍应分别研究。

## 验证

数学不变量和源抽取测试覆盖：零阈值与生产计算逐结构／数值相同；基线输出直接保留已发布方向；删国后真正重算所有分母；ck 先零化 X；同步三轮级联删除；相同最终 M 投影 PCI；断连不造分；阈值相等保留；RCA=1 容差；引用事件门槛不误施于论文；UNKNOWN 学科进入 whole 支持量而 Concepts 不重复求和；非法原值不能藏在筛后行；竞争并列、可空无效 rank 经 Parquet 往返保持；整链同 factor 与双侧共同翻转；2026 不参加因子选择；共同国家顺序／国家重命名不改变选择；弱／不可用参考明确披露。

```sh
go test ./internal/complexityfilter ./scripts/complexity-profiles
```
