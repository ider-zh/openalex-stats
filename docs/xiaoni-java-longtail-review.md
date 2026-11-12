# 小倪原 Java：长尾国家处理专项

核查日期：2026-11-13。只读本机 Windows 原项目 `G:/ni/workspace/selenium_shuzhi_IDEA`，没有运行旧 Java、连接旧数据库或改网站／计算结果。另两份 `selenium_shuzhi`、`selenium_shuzhi_20260519` 的三个主要 Java 文件已核 SHA，逐文件与 IDEA 版相同；目录日期不能当算法版本。

**确实找到会让长尾国家从榜单消失的处理，而且不只一种：末端按优势学科数筛榜、硬编码国家展示名单、固定参考年的 TopN 国家集合，以及备选建模路线里的国家总量／单格绝对量门槛。最直接的末端处理不修改复杂度分数，只跳过对象并重新编号。** 但没有找到匹配的历史输出与运行参数，不能据此断言小倪最终历史图实际用了哪套规则，更不能把这些备选函数的全部处理合称为当前启用算法。

## 1. 最直接的处理：高分低多样性对象不准进入榜首

在 [FzxByCtry.java:9715](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:9715) 的 `eciPci_new_jg` 中，终端输入 `kc0_filter`、`kc0_top100`、`kc0_top100_2000`（9730—9735）。`kc0` 来自二值优势网络行度：**有优势的学科个数，不是论文数、引用数或不同被引目标数**。旧代码该路线多用细分 Concepts level 2，其阈值不能直接套到当前仅 19 个大类的 Concepts 0。

| 条件 | 精确行为 | 位置 |
|---|---|---|
| `kc0 > kc0_filter` | 严格大于才输出；等于也删除 | [9875](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:9875) |
| `index < 100 && kc0 < kc0_top100` | 在前 100 个通过者尚未凑齐时，跳过低多样性对象；等于阈值允许 | [9877](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:9877) |
| `year >= 2000` 且同类条件 | 固定 `dd>0` 路径额外要求 `kc0 >= kc0_top100_2000` | [9880](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:9880) |
| 通过后 `index++` | 重新连续编号；被跳过者不占前 100 名 | [9885](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:9885) |

这不是“只审查原排名前 100”，也不是“最多输出 100”。程序在通过 100 个对象之前一直执行较高门槛，可能跳过原排名第 101 之后的对象；凑齐后较低多样性对象可按全局门槛继续输出。如果所有对象合计都不足 100 个通过较高门槛，其余对象全部不会输出。

`dd<=0` 的自动选轮路径也有全局门槛和前 100 通过者门槛（9902—9913），但没有固定 `dd` 分支中的 2000 年后额外门槛。不能把两条路径说成完全一样。

**这是后处理筛榜。** 全体输入分值的均值与中心化 L2 范数在 [9835—9853](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:9835) 已计算；筛后只用原均值／范数求输出分值（9886—9888），不重建 M，不重算反射，不重新标准化剩余对象。因此它可让图看起来更符合预期，但没有解决原网络中的误配或低支持边。

学校类 [universityFzxByCats.java:7622](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/universityFzxByCats.java:7622) 也有同名参数。国家类沿用很多学校注释和变量，说明这是可复用实体筛选逻辑，不能只凭方法名视为已采用的国家专用统计标准。

## 2. 还有两种明确的国家展示截取

### 硬编码国家名单

[EconomicsFzx.getFullByDdFile:1285](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/EconomicsFzx.java:1285) 写有 `arr40`／`arr50`。前者实际 **41 个唯一国家代码**，后者 **50 个**；两者均没有 AG、EH、KP，均保留 US、CN、SG 等。循环把已算好的 `tjdata_zh_国家_阈值=10.txt` 按名单导出为 `tjdata_zh_top40.txt`／`top50.txt`，并重新编号（1302—1321）。国家列表与计数已另存[纯汇总](../research/xiaoni-java-longtail-review/source-locations-and-display-arrays.json)。

这是“固定国家子集中的显示序号”，不是全体国家模型的全球排名。没有重新计算分值／矩阵。若历史图使用这类文件，小国家可能只是没有显示；本次未找到实际图与这些文件的绑定证据。

还有一个显著实现缺陷：`arr40` 有 41 国，编号却在 `index_40==41` 时回到 1，即按 **40 条输出记录**循环；`index_40` 不是按年份重置。若一年 41 国齐全，最后一国会重复编号，下一年还会错位。`arr50` 虽为 50 国，但缺国年份也可能因按记录数回绕而跨年错位。这种写法不可借鉴为排名实现。

### 固定参考年 TopN

[EconomicsFzx.getFullByDdFile_topN:1191](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/EconomicsFzx.java:1191) 读取用户输入的参考 `year`、`N`，先从该年的输入记录取前 N 个国家（1220—1225），再跨年只导出这组国家（1237—1244）。它固定了观察集合，可能让其他年份领先的小国家完全不出现，仍不重算 M／分值。输出编号也是每 N 条记录回绕，不按年份校准，缺国或排序／记录数异常时不稳。

这两个展示函数在 `EconomicsFzx.main` 的 131、132 行均已注释。源码证明具备导出能力，不证明历史实际启用。

## 3. 真正会改变网络的备选处理

| 路线 | 精确条件与作用 | 当前入口状态／限制 |
|---|---|---|
| 国家总量＋单格量 | [EconomicsFzx.getMcpMap_year_test:2904](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/EconomicsFzx.java:2904) 输入 `zck`、`ck`；[3013](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/EconomicsFzx.java:3013) `c_t < zck` 分支主体全注释，整国不入 M；达到总量门槛后仅保留 `cc >= ck`，再用原世界比例判优势（3039、3077） | `main:87` 已注释；不是有效常规路线。过滤后未重算原国家总量／世界比例。`c_t` 由旧 TSV 中引用 ID 列表长度累加而来（2983—2990），不能冒称当年去重论文数 |
| 绝对单格出口量替代 RCA | [FzxByCtry.getMcpMap_year_by_tmpdata_year_jg:4011](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:4011) 只以 `dd > ck` 建边，弱格被删，无边行隐式不入网络 | `main:69` 已注释；不是 RCA≥1 再叠加门槛，而是另一建边定义 |
| 世界均量替代 RCA | [FzxByCtry:4817](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:4817) 需 `dd > avg && avg > avg_min` | `main:71` 已注释；改变研究对象，不是标准 RCA 同义实现 |
| 优势阈值 `qz` | [FzxByCtry:5871](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:5871) 保留 `country_share >= qz * world_share` 且世界份额正 | `main:67` 已注释；`qz` 来自交互输入，实际数值未知。提高 RCA 门槛不保证大国靠前 |
| 学科／原论文范围筛选 | 最新联盟分支论文 `linksin >= filter_linksin`（13078），学科需论文量排名及引用量排名**同时 ≤topN**（13127—13140），引用两端都需属于筛后集合（13449—13451） | `main:60` 有效，但 `topN` 这里筛的是学科，不是国家。`filter_linksin` 也不是国家总样本门槛 |
| 五年端点窗口 | 同分支 `from0=year-4,to0=year`（13197—13198），引用记录需 `py0∈[from0,to0] && py1>=from0`（13447） | 可能积累较多样本，但同时改变两端年份／引用范围，不等于本研究当年被引；不应无提示替换 |

备选 `getMcpMap_year_test` 还在 [3036](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/EconomicsFzx.java:3036) 硬排 `Korean culture`，属于指定学科删除，不是长尾国家通用可靠性方法。

另一备选 `FzxByCtry` 绝对量路线还可删既有 PCI 第 3 轮排名前 `top` 的学科（4048—4065），并用硬编码 22 个数值实体 ID 的 `zh` 集合，删除优势实体全部属于该集合的学科（4035—4037、4078）。这些数值 ID 不是国家代码，应按原实体解释，不能称“把中国国家筛掉”；该路线同样未在当前 main 启用。

普通国家输入名单也不是隐藏的“国家产量 TopN”。[jsData.getArtByCtry:1854](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/openAlex/jsData.java:1854) 对所有出现国家计数、排序后**全部写出** `openalex_ctry_artcount_nopb_notypenull.txt`（1860—1863），没有数目或篇数阈值；[FzxByCtry:10746](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:10746) 再全量读作 `topN` 集合，变量名不能证明只取头部。后者从 `_onectry.txt` 每 Work 仅保留首次归属（10765—10771），不是当前全合作国家归属；这种改变也可能减少小国合作者所得的记录，但不构成已验证的国家可靠性筛选。

## 4. 看似有过滤，实际未作用于复杂度网络的地方

`getMcpMap_year_jg_filter` 标称“过滤只有 n 个学校才能出口的学科”（5953）。它先用原 `kp0 > gb` 组成 `set_more`（6076），但 [6162—6178](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:6162) 写 `mcp_data` 时没有套用该集合。直到 [6191](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:6191) 才过滤后续 `kc0`／`kp0` 计数文件。

常规反射 `DdFzx_bl_year_jg` 仍在 8249 重读未裁剪的 M，并由实际边重新初始化度。因此该 `gb` 没有按注释真正删掉反射矩阵列，只改变辅助度数文件，可能再影响末端筛榜。若照搬，可能出现“用于展示筛选的 kc0 与实际迭代网络度数不同”。该函数当前 `main:66` 也已注释。

`EconomicsFzx.getMcpMap_year_deleteGL`（2846）和 `DdFzxFilterExport`（684）只是空方法；不能从名称宣称有最大连通块或其他过滤保护。核查的常规反射／标准化链未发现贝叶斯收缩、伪计数平滑、按论文支持量加权或完整连通性保障。发现的零量排除、归一化或固定轮次不等于解决长尾国家可靠性。

## 5. 启用状态与运行证据

原项目由一组读写磁盘 TSV 的交互式方法组成，不是自动完整流水线。`FzxByCtry.main` 当前有效的是联盟预处理（60）和两网络非对称反射（80），常规建网、选轮、末端筛榜均为注释；两步还依赖不同历史中间目录。`EconomicsFzx.main` 当前同时可见常规建网（90）、反射（93）、选轮（94）、标准化（103）调用，但其 `getMcpMap_year`（3259）没有上述整国 `zck` 门槛，标准化（454）也没有 `kc0_top100` 后筛。不能把不同类、不同可选函数的配置拼成一套“Java 默认长尾修复”。

有界搜索了原项目树、`G:/桌面` 与有关展示目录，未找到对应 `ecipci`／`tjdata`／`kc0` 国家结果、参数文件或可证明规则实际采用的运行日志。源码指向的 `C:/Users/Administrator/Desktop/some`、`C:/桌面/jar/parse_ctry`、`G:/桌面/jar/parse_ctry` 均不存在；IDEA `workspace.xml` 未保存该类 RunManager 配置。没有搜索整台机器所有资料，以上不是“历史文件绝不存在”的证明。

所以：实际 `kc0_filter`／`kc0_top100`／`zck`／`ck`／`qz`、历史最终榜所用源和类别层级，仍为未知。不能把提示里的 `[100,1000,10000]`、旧目录名或示例参数当实际运行记录。

当前普通 `EconomicsFzx.getRankByYear` 另有 US 单参照停轮：只看美国的偶数序列（3897），按 `avg*0.002` 或输入 `yz` 判两步变化（3917—3929），再让所有国家取同一轮。US 缺失时可能留在第 0 轮；这不是全向量收敛，也不是直接筛长尾。不能把它当成优于当前固定 16 轮的现成稳健性方案。

## 6. 对当前系统可借鉴什么

1. **可借鉴“明确准入与可靠性”的思路。** 优势学科数可以作为诊断维度，但还必须同时报告去重论文数、不同被引目标数和单篇集中度；不能把 `kc0` 当科研样本量。阈值需按四层分类分别预声明、做稳定性验证。
2. **模型筛选与展示筛选应分开。** 前者改变网络，需独立版本、完整重算 RCA／M 及诊断；后者保留原全球排名，在界面明确显示“筛选后的子集”，不重编号成全球榜，不用人工国家名单暗中隐藏异常。
3. **窗口与单一国家归属只能另立口径。** 它们可能降低噪声，却也改变研究问题，不能借“复现小倪”把当前当年被引和合作国家口径悄悄替换。
4. 不借鉴硬编码学科删除、按预期榜首调参、空过滤方法或度数文件／实际网络不一致；不假设旧代码已经系统解决了长尾。

根代理另对当前冻结矩阵作了**示例而非历史参数**的末端筛选：Core 2025 Concepts 0 当年被引，小倪方法论，设 `kc0_filter=0`、两项前 100 门槛为 3。229 个对象剩 227，AG／EH 被跳过，但前列随即成为 SX、MQ、PR 等，所有保留分值完全未变。它说明这类显示处理能藏掉特定极端对象，不能验证剩余榜已反映国家能力。输入 SHA、原／后榜及说明见 [display-filter-illustration.json](../research/xiaoni-java-longtail-review/display-filter-illustration.json)，复算脚本见 [display_filter_probe.py](../research/xiaoni-java-longtail-review/display_filter_probe.py)。

源位置、SHA、名单和有界运行证据记录见 [source-locations-and-display-arrays.json](../research/xiaoni-java-longtail-review/source-locations-and-display-arrays.json)。本报告只增加可审查文档，没有改动原 Java 或当前统计／网站。

计算前筛选的独立复核见 [precompute-independent.md](../research/xiaoni-java-longtail-review/precompute-independent.md)，其源码哈希见 [precompute-source-hashes.json](../research/xiaoni-java-longtail-review/precompute-source-hashes.json)。原方法整体差异见 [java-method-diff.md](java-method-diff.md)；本次以重新读取源码后的明确条件和入口边界为准。
