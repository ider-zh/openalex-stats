# 本机 Java 知识复杂度与当前 Go 方法差异

核查日期：2026-11-12。仅静态阅读本机 Windows 源码，另运行了一个固定小矩阵的纯 Go 算术对照；没有执行旧 Java 应用、连接旧数据库或修改生产算法。源码位置、SHA-256 和方法范围见 [locations.json](../research/java-method-diff/locations.json)。

**结论：找到了真实实现，但旧项目含多套方法，不能把整个 Java 项目概括为一个 ECI 算法。** 常规链条使用二值优势网络上的有限次双向反射迭代；当前 Go 使用第二特征向量。两者在满足条件时存在数学联系，但有限迭代、取第几次、标准化尺度、学科/国家归属及年度窗口均会带来差异。最近 Java `main` 又启用了两网络交叉的“非对称复杂度”，这与当前单矩阵 ECI 更不是同一模型。

## 1. 定位与实际入口

主要源码目录：`G:\ni\workspace\selenium_shuzhi_IDEA\src\main\java\com\shuzhi\EconomicsFzx`。

| 文件 | 核查用途与证据 |
| --- | --- |
| [EconomicsFzx.java](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/EconomicsFzx.java:46) | 明确注释“经济复杂性套用 mag”；较早的 OpenAlex/MAG 链条，RCA 网络 `getMcpMap_year`（3259）、反射迭代 `DdFzx_bl_year`（4113）、归一化 `eciPci_new`（454）。`main` 103 行当前调用归一化步骤。 |
| [FzxByCtry.java](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:44) | 更晚的国家/学校/联盟研究集合。`main` 50–102 行保留有编号的历史工作流；当前有效调用是 60 行 `getMcp_end_filter_year_jg_union_EU` 和 80 行 `DdFzx_bl_year_jg_union`。两步依赖不同中间文件，**不能仅凭 main 推断它们当时成功完成过端到端生产运行**。 |
| [getSomeFzxListUniThread.java](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/thread/getSomeFzxListUniThread.java:435) | 真实计算学科引用量与国家×学科引用量：对 `map_country_linksin` 的集合大小求和，再按 Work 是否属于学科加入。 |
| [Economics_openAlex.java](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/Economics_openAlex.java:196) | 另一套 OpenAlex 预处理/反射迭代实现；它不是前两个类的统一公共算法库。未把其结果与 `FzxByCtry` 混称为同一次研究。 |

同机还找到 `G:\ni\workspace\selenium_shuzhi`、`G:\ni\workspace\selenium_shuzhi_20260519` 两份项目。三份项目中的 `EconomicsFzx.java`、`FzxByCtry.java` 均逐文件 SHA 相同，不能将目录日期视为算法版本差异。这里只选 IDEA 目录作稳定引用。源码内出现 Linux/229 路径属于旧程序配置字符串；本次没有到 229 搜索 Java。

## 2. 可核查的方法链

Java 常规路线，步骤之间通过磁盘 TSV 文件衔接，**不是一个函数自动调用全部步骤**：

1. `FzxByCtry.getMcp_tmpdata`（10653）读取 Work→单一归属方中间表，构建归属与引用集合。
2. `getMcp_end_filter_year_jg`（11774；以及最新联盟变体 13014）根据时间窗口、学科与引用阈值形成国家×学科量及国家/世界比例。
3. 生成二值网络有多种互斥选择：`getMcpMap_year_jg`（5628，比例优势阈值），`getMcpMap_year_by_tmpdata_year_jg`（3762，绝对出口量阈值），`..._cgbl`（4164，重新计算多标签分母），`..._worldavg`（4511，世界平均量阈值）。
4. `DdFzx_bl_year_jg`（8176）读 `mcp_data_...txt`，输出每个实体第 0 至 dr−1 次反射值。
5. `getRankByYear_jg`（9351）选某次迭代，或依据一个参照实体决定停止次数。
6. `eciPci_new_jg`（9715）归一化，并可过滤低 kc0/排名前 100 的实体。

各函数实际选择了哪些参数、对应哪批历史结果文件，**unknown**：本机源码不足以恢复当时终端输入。不能认定“同事实际采用了所有变体”或把默认注释当成运行记录。

当前 Go 链：`scan/parquet.go` 读维度 → `policy.Scope.Includes` 选 Q → `graph.Count` 计算引用事件 → `aggregate.Aggregator` 形成四层国家×年度×学科 cube → `pipeline.ComputeModels` 分层/分年/分指标 → `eci.Compute` → 跨年符号对齐。见 [run.go](../internal/pipeline/run.go:125)、[models.go](../internal/pipeline/models.go:27)。

## 3. 数据口径与矩阵差异

补充核查：已继续找到 `_onectry.txt` 的同名生成器及旧 MAG 文本补全链，详见第 8 节。下表中国家归属生成规则的 unknown 已由该补充部分细化；历史生产是否使用这一源码版本仍需运行记录确认。

| 维度 | Java 实际证据 | 当前 Go 实际证据 | 对结果的影响 |
| --- | --- | --- | --- |
| 文献集合 / 扩展数据 | Java 用 20241031 路径和预制 TSV；`FzxByCtry` 10746 的文件名含 `nopb_notypenull`，但仅凭名称不能恢复精确类型规则，XPAC 规则 unknown。 | [policy.go](../internal/policy/policy.go:12) 明确 15 类黑名单，40 行另排除 `is_paratext`；core 排除 XPAC，all 包含；`CorpusYearStart=0` 保留早期被引文献。 | Q 不同会同时改变文献、引用边及 RCA 基准。旧文件名不构成与当前黑名单等价的证据。 |
| 国家归属 | [FzxByCtry 10756](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:10756) 注释优先通讯作者、其次 last/first，实际读 `_onectry.txt`，10765–10771 只保留首次出现的一个归属。该方法其他历史分支的 country 甚至指学校（10700）。生成 `_onectry` 的完整优先规则此次未定位，unknown。 | [parquet.go 407](../internal/scan/parquet.go:407) 汇总全部 authorships.countries、排序去重；[aggregate.go 105](../internal/aggregate/aggregate.go:105) 再去重，国家 fractional 为 1/n，whole 为 1。 | Java 此分支是单一归属，不能映射成 Go 的国际合作均分或各国全计数；更不能把学校 ID 当国家代码直接比。 |
| 学科体系/范围 | [FzxByCtry 10675](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:10675) 调用旧分类过滤；5628 路线提示 level 1/2、可选大类再在子类上建网络；5684 还列出临床类过滤。未发现该路线按 Topics Field/Subfield 统计的证据。 | [models.go 27](../internal/pipeline/models.go:27) 明确 Topics Field、Subfield，Concepts 0、1，分别建模；[aggregate.go 105](../internal/aggregate/aggregate.go:105) Topics 取 primary_topic 的单一 field/subfield。 | 旧三级 Concepts 子类网络与当前四层网络研究问题不同；层级数字也不可直接互换。 |
| Concepts 多标签 | [线程 459](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/thread/getSomeFzxListUniThread.java:459) 一篇 Work 属于哪个学科，就向该学科加入完整 linksin，不均分；标签在上游集合内去重。旧标签 score 阈值 unknown。 | [aggregate.go 110](../internal/aggregate/aggregate.go:110)、[237](../internal/aggregate/aggregate.go:237) score 达阈值后同层 ID 去重，默认每标签全计数；[counting-policy.md](counting-policy.md:5)。 | **“命中各标签全记”方向一致**，但层级/阈值/上游过滤不同，不能说输入等价。 |
| 年份窗口 | [FzxByCtry 13194](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:13194) 以 year 为端点，from0=year−4、to0=year；13447 条件为 py0∈[from0,to0] 且 py1≥from0。13439–13444 指明两列年份，但该中间表两端生成定义尚未完整核查，故不武断指定 py0 是施引还是被引。 | [graph.go 274](../internal/graph/graph.go:274) 按施引 Work.Year 统计当年引用；315 行写入该年目标 Work 计数；[aggregate.go 124](../internal/aggregate/aggregate.go:124) 发表按发表年。研究要求从 2000 起输出，早期被引对象保留。 | Java 该变体是五年端点窗口，并对两端年份施加下界；不是当前逐年事件统计，可能漏掉较老被引论文。 |
| 引用两端/自引 | Java 最新变体 [13449](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:13449) 要求两端都在选定大类与 needids；needids 由 13078 的 linksin 阈值得到。线程旧原始引用分支 [282](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/thread/getSomeFzxListUniThread.java:282) 虽计算国家交集，但排除交集的 if 在 290 行已注释，不能据注释断言排除国内引用。 | [run.go 125](../internal/pipeline/run.go:125) 与 174 两次选同一 Q；[graph.go 300](../internal/graph/graph.go:300) 引用去重、305–308 按配置排除同 Work 环；作者自引和国内引用不因此排除。 | Java 可能是大类内引用/筛选后出口网络；Go 是 Q 内所有合法引用归属于被引端国家/学科。国内引用是否保留须按分支判断。 |
| 原始权重 | Java [13460](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:13460) 用国家→Work→引用 ID 的 HashSet，集合大小是全计引用数；[线程 435](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/thread/getSomeFzxListUniThread.java:435) 按归属集合统计。 | [aggregate.go 251](../internal/aggregate/aggregate.go:251) count×国家权重×标签权重，支持发表量、被引量两套。 | Go 国家 fractional 允许小数，whole×Concepts whole 的单元格为整数；ECI 本身始终是连续值。 |
| RCA 分母 | Java 比例路线 [13583](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:13583) 世界分母是按国家累加的去重 Work 引用事件总和（13480），国家分母同样对每 Work 计一次（13590）；学科分子可重复归属。 | [eci.go 71](../internal/eci/eci.go:71) 从 X 的所有单元格直接求行和、列和、总和；102 行 `(Xij/row_i)/(col_j/total)`。 | 多标签时 Java 该路线使用事件分母，Go 使用**学科归属次数分母**，一般不等价。若各国标签重叠倍数不一样，优势判定可能改变；这是研究定义差异，应 review 后再决定。 |
| RCA 分母的另一变体 | Java `..._cgbl` [4329](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:4329) 重新累加每个国家×学科单元格得到 world_total、国家合计、学科合计，4353 用学科/总和。 | Go 同上。 | 该变体在**输入 X、保留行列和阈值全部相同**时，RCA 分母结构与 Go 相符；不能说全部 Java 变体都与 Go 分母不同。 |
| 二值化 | Java [5871](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:5871) 比例≥qz×世界比例，qz 可输入；较早 [EconomicsFzx 3385](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/EconomicsFzx.java:3385) 比例≥世界比例且世界比例>0。另有 [4011](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:4011) 绝对量>ck 和 [4817](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:4817) 大于世界平均量的变体。 | [eci.go 102](../internal/eci/eci.go:102) 固定 RCA≥1；[15](../internal/eci/eci.go:15) 1e−12 临界舍入容差。 | 只有 qz=1 的同分母比例路线才是相同 RCA 规则。绝对量/世界均量路线不能称为标准 RCA。Java 未见相应舍入容差。 |

特别注意：Go 多标签情况下国家“论文总量”不能由各 Concepts 学科求和得到，二者分别是去重 Work 数与学科归属次数。当前 [计数口径](counting-policy.md:18) 已明确重新区分；这也应与 ECI 行和分母分开说明。

## 4. ECI/PCI 核心数学 diff

设二值优势矩阵为 M，D_c=diag(k_c0)、D_p=diag(k_p0)，k_c0=M1、k_p0=Mᵀ1。

Java 常规反射迭代，见 [8297](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:8297)、[8316](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:8316)：

```
k_c(n) = D_c^(-1) M k_p(n-1)
k_p(n) = D_p^(-1) Mᵀ k_c(n-1)
```

两侧都读上一轮，并不是把本轮刚算的国家值立即用于学科。本轮边权均为 1/节点度，不使用原始 X 的强度。消去中间一步得 k_c(n)=T k_c(n−2)，T=D_c⁻¹ M D_p⁻¹ Mᵀ。

Go [144](../internal/eci/eci.go:144) 构造对称相似算子 S=D_c⁻¹/² M D_p⁻¹ Mᵀ D_c⁻¹/²，取第二大特征值的特征向量 u，再用 e=D_c⁻¹/²u 恢复 T 的右特征向量（[178](../internal/eci/eci.go:178)）。因此**D 归一化与二步反射算子有联系**，但 Go 直接剔除常数主模态，Java 有限次反射值保留全部模态并接近常数。

| 项目 | Java 常规方法 | Go 当前方法 | 影响 |
| --- | --- | --- | --- |
| 特征向量 / 迭代次数 | 不做特征分解；dr 控制轮数，dd 指定取第几次。 | [eci.go 156](../internal/eci/eci.go:156) 对称特征分解，固定取第二大，即升序 a−2。 | Java dd=4 等是有限迭代指标，不等同于直接解谱 ECI。 |
| 何时近似一致 | 同一 M、连通、第二特征值单一、初值在对应模态投影非零、固定奇偶子序列且未发生浮点消去，再中心化和统一尺度/符号时，反射序列可能趋近第二模态。此为上面公式的推导，不是旧程序保证。 | 有重根/退化诊断，不依赖某个初值恰有投影。 | 不保证所有矩阵或所有 dd 排名一致；长迭代还会因接近常数而丢失有效差异。 |
| 自动选择次数 | [getRankByYear_jg 9454](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:9454) 国家侧只查看 ID=31922（注释 Harvard）的序列，学科侧只查看 Population。9475 用初始度的全体平均×0.002 或输入 yz 为停止阈值；9480 国家挑偶数、学科挑奇数；9498 允许 dd 覆盖。 | 固定第二特征向量，另报告残差、eigengap。 | Java 自动停不是全向量收敛检验；参照实体缺失时 dl 为空，index 仍为 0，可能实际选择初始度。dd 覆盖也可能破坏奇偶约定。 |
| ECI/PCI 方向 | 国家与学科可独立选 dd，自动路径用国偶/学科奇处理方向；未在此链见基于全体国家的相关方向或跨年对齐。 | [eci.go 195](../internal/eci/eci.go:195) 首年与国家多样性正相关；相关接近零时按输入顺序定号；[models.go 153](../internal/pipeline/models.go:153) 后续年份按共同国家协方差翻转 ECI/PCI。 | 同 dd 或不同奇偶可能令学科排序反向。Go 跨年会共同翻转 ECI/PCI，也可能使当年与多样性相关为负，属于已定义的连续性选择。 |
| PCI 公式 | 输出独立反射序列 k_p(dd)，用其自己的均值/范数中心化。 | [eci.go 224](../internal/eci/eci.go:224) 从**未标准化且已定号的国家特征向量**计算 p=D_p⁻¹Mᵀe，再单独总体 z-score。 | 与 Java 学科奇数序列的极限有联系；不能将 Java 同一偶数 dd 的 PCI 直接拿来比较。 |
| 标准化 | [eciPci_new_jg 9845](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:9845) 均值正确；分母为 sqrt(Σ(v−均值)²)，9851 没有除 N，也没有除 N−1；9888 输出中心化/该范数。较早 EconomicsFzx 555–574 相同。 | [standardize 259](../internal/eci/eci.go:259) 分母 sqrt(Σ(v−均值)²/N)，是总体标准差。 | 即便 v 完全一样，Go 分数=Java 分数×sqrt(N)。Java 分数总体标准差为 1/sqrt(N)，不是 1；国家数量变化会带来额外年度尺度变化。不能称为“样本与总体标准差小差别”。 |
| 年度/网络实体集合 | Java 只从有边记录构建集合（[8244](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:8244)）；归一化之后还能按 kc0、前 100 阈值过滤（9875）。 | [dictionary.go 267](../internal/results/dictionary.go:267) 用固定词典填零；[models.go 49](../internal/pipeline/models.go:49) UNKNOWN 不进模型；[eci.go 97](../internal/eci/eci.go:97) 仅正量行列参与谱计算。 | 固定标签集合不等于每年有效样本数固定；Java 显示后过滤不重算均值，导出子集均值不一定为零。 |
| 0 / 缺失 | Java 普通有边网络隐式移除零度节点；归一化分母为 0 无专门处理，Double 除法可能输出 NaN/Infinity。 | 输入必须有限非负；零量/单行单列 empty，零方差 degenerate，不伪造 0 分数。 | Java 数值输出存在不代表有可比较 ECI；Go 缺失分数是显式不可定义。 |
| 断连 / 重根 | 已读 Java 常规迭代及归一化方法未发现连通分量、重根、eigengap 或全局可比性检测；只是继续逐分量迭代。 | [eci.go 169](../internal/eci/eci.go:169) disconnected 不给全局 ECI/PCI，173 重复第二特征值 degenerate；167 小 eigengap 标不稳定。 | Java 可能把不同连通块初值/度量残留排成一个榜；这种榜与 Go 全局 ECI 缺失不是计算失败的同义情形。 |

## 5. 最新启用的非对称路线须单列

[FzxByCtry 6742](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/FzxByCtry.java:6742) 的 `DdFzx_bl_year_jg_union` 分别读取国家与联盟两套优势网络，分别构造行侧/学科侧初始度（6885–6921）。国家下一轮值从**另一套网络**的学科值取平均（6951–6961），另一侧也反向使用对方网络（6986–6996）；学科值则从各自网络的行侧上一轮取均值。

这不是对一个 M 求 T 的标准反射法，也不是 Go 当前 ECI 的实现差异，而是引入另一网络参照系的模型扩展。两套学科不完全重合时只对匹配学科计 size；size=0 的分支在 6961、6996 未防除零。实际采用了哪个联盟、哪些年份、哪套输入仍为 unknown。不能将它作为“应自动替换当前 Go ECI”的依据。

## 6. 固定同一输入的数值对照

独立 [Go 对照程序](../research/java-method-diff/numeric/main.go:1) 仅翻译常规 Java 迭代/范数算术，并调用当前 `eci.Compute`；不调用旧 Java 的任何入口。执行 `go run ./research/java-method-diff/numeric` 成功，完整结果见 [numeric-comparison.json](../research/java-method-diff/numeric-comparison.json)。

输入 X，行 A/B/C，列 x/y/z：

```
10  1  1
 5  5  1
 1  5 10
```

Go RCA≥1 得 M=`[[1,0,0],[1,1,0],[0,1,1]]`；为隔离算法差异，Java 翻译使用**同一个 M**，不涉及上游分母争议。

| 国家分数（A,B,C） | 结果 |
| --- | --- |
| Go 谱 ECI，总体 z | −1.118034, −0.190983, 1.309017 |
| Java 反射 n=4，按旧中心化 L2 范数 | −0.655610, −0.093659, 0.749269 |
| 相同 Java n=4 改用总体 z，仅隔离轮数差异 | −1.135550, −0.162221, 1.297771 |
| Java n=16 改用总体 z | −1.118034, −0.190983, 1.309017（至六位） |

该例说明有限次误差与 sqrt(N) 尺度差异是两件事，不能通过改一个系数解决所有差异。n=1、3 的国家方向与偶数轮相反；同一偶数 n 的 Java 学科方向与 Go PCI 相反，而 Java 的“国偶/学科奇”选择有明确源码依据，不能忽略后笼统称“Java 方向错误”。

## 7. 建议 review 的决定，不在本次自动改动

1. 保留当前 Go 谱 ECI 作为研究主模型；若要复现同事历史结果，单独命名 `legacy-reflections`，把 dr、dd、国家/学科奇偶、旧范数、参照实体、RCA 路线/分母、数据版本都写入结果元信息。**这是建议，尚未实施。**
2. 首先决定 Concepts 多标签时 RCA 的分母：采用当前“国家×学科归属量行和”，还是“去重论文/引用事件总量”。两种都有明确含义，但不是等价实现；旧项目自身也同时存在两条路线。
3. 若只希望沿用旧方法论思想而不严格复现，建议沿用二值 RCA 网络及 D 归一化关系，继续用 Go 的谱诊断、总体标准化、跨年定号；不要无意复制旧的 sqrt(Σ偏差²) 尺度。
4. 旧代码固定大类、在其后代细类上分别建网络，会产生“生物学内部国家复杂度”。当前四层 ECI 是该层全部学科组成的国家复杂度，PCI 是每个学科的复杂度。两者应在产品中区分命名；不能把国家全学科 ECI 误标成每个所选学科独立的 ECI。
5. 非对称/联盟模型如有研究价值，应另立模型及验证。尚无证据支持将其直接合并替换当前单网络算法。

本次可确定的是源码数学差异；同事**当时实际运行参数、旧中间数据生成时的精确文献类型/score/国家规则、输出生产版本**仍需旧运行日志/配置或本人确认。报告保留这些 unknown，不据目录名、注释或残留 main 推断运行事实。

## 8. 国家识别链补充：哪些值得用于覆盖率增强

本节为 2026-11-12 对国家生成链的后续静态核查。没有运行旧 Java 程序、调用旧地理服务或采用旧词典结果。新发现文件的 SHA 与范围见 `research/java-method-diff/country-chain.json`。

### OpenAlex：机构国家与作者角色选择

[jsData.getArtByCtry](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/openAlex/jsData.java:1795) 先从旧中间表的 authorships.countries 收集可用国家代码，再交给 [getSchArtOpenAlexThread.getOneCtry](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/thread/getSchArtOpenAlexThread.java:221)。后者读取本篇论文 `authorships.institutions[].country_code`（279、345 行），不依赖作者当前机构。这是值得纳入补全证据的结构化字段；它相对当前 countries 集合的全量额外覆盖仍需实测。

作者角色选择是在已有可识别机构国家中优先通讯作者，否则写 last 和 first，有必要时回退首个有机构国家的作者。first 与 last 不同国家时可能写两行，文件名 onectry 不能证明逐 Work 唯一；下游首次出现规则又可能只保留一行。一个作者有多个机构时，局部 schid 被后一个命中覆盖。这些选择属于归属政策，不会凭空补出缺失国家，也不适合直接代替当前全部署名国家去重集合。

### MAG：无机构 ID 的原始机构字符串补全

[EconomicsFzx.getCountryByNoOrgId](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/EconomicsFzx/EconomicsFzx.java:2490) 读取三类词典：

- `country_redirect_list.txt`：国家名称、重定向/别名，另补中国、美国、USA 等。
- `sch_names_target_20220930.txt`：机构名称、别名与中文名。
- `sch_country_map.txt`：机构名称到国家的映射。

方法用词边界正则匹配没有 org_id 的原始 org 文本，输出 `noOrgId_country_*.txt`。随后 [wiki.parseXml.getMag_country](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/wiki/parseXml.java:5121) 仅保留候选国家集合大小为 1 的文本映射（5164），对没 org_id 但有 org 字符串的作者做精确查表（5237）。这是旧 MAG 数据路线，尚无证据证明它曾接入所检查的 OpenAlex onectry 路线。

值得借鉴的是“国家名称之外，再用完整机构别名匹配机构国家”及“多候选进入歧义处理”。只有学校名、未出现国家名的署名文本也可能因此得到候选。当前目录搜索尚未找到这些旧词典本体，不能认定其覆盖范围、准确率、时代适用性或可直接使用。

旧实现不能原样复制：2625 行条件写成 `country != null && country.equals("")`，非空学校国家反而不会加入；词条未做正则字面转义；同 target 多次 put 会覆盖之前的别名集合。全文词边界匹配也不能解决 Georgia、跨境分校、学校品牌名称含国家等歧义。仅一个候选不等于候选正确。

### MAG：机构坐标反查国家

[getCountryByMag.getCountryAndCityByXY](G:/ni/workspace/selenium_shuzhi_IDEA/src/main/java/com/shuzhi/mag/getCountryByMag.java:172) 和 `getCountryByXY`（270）从机构数据取经纬度；`getCountryCode`（368）查询旧本地 geocode 服务返回 countryCode。思路可用于机构已有可靠坐标、国家字段缺失的情况。当前快照应先检查 `institutions.geo.country_code`；若进一步用坐标，需要独立地理边界、坐标有效性、海外校区及边界附近歧义验证。没有执行旧服务或验证旧输出准确率。

### 本次评估采用方式

已把这些发现交给后台国家补全 agent，作为独立来源/规则候选评估，保持原始 countries 与生产版本不变。机构字段、机构 ID 查表、机构别名文本映射、直接国家文本及可能的地理证据应分别报告增量和重叠；作者角色选择不能计为覆盖率提升。需要最终全量评估与人工 review 后，才能判断哪条规则值得进入增强版统计。
