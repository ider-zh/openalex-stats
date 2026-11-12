# 独立完成差距审查

审查日期：2026-11-12（Asia/Shanghai）。依据：[Goal契约](goal-prompt.txt)。本次同步v7正式完成、全cell等价、216模型独立复算、正式幂等重放、CPU窗口独立积分和整数产物摘要比较。

**最终独立签核：交付证据通过，未发现未说明的实现、统计验证、性能或展示阻塞。**v7正式结果、同配置replay、完整cell/216模型/整数摘要、v8校验基准与测试/race、实际v2服务24项browser及发布清单均已核对。签核保留明确限制，由root执行最终归档刷新和Goal状态更新。性能结论是代表性60秒窗口平均达到目标，包含短时下降，不能改写为每秒或所有阶段均达到90%。

审阅方式：读取代码、小型JSON、方法/性能报告及已有浏览器记录；SSH只读正式元数据，没有大型Parquet扫描或重CPU任务。本审阅者参与过metrics实现，因此不将自己的实现/单元测试当作独立数值oracle；全量SQL和一般非对称特征值复算另有独立程序。追加raw投影审计只读取两个明确分区，范围另述。

## 1. 正式运行身份

| 项目 | 基线core-goal-v1 | 优化core-goal-v2 |
|---|---|---|
| 统计程序 | v4 | v7 |
| run_id | `65644017413394b988bb5f1e81e7e4a055e2a119f4463650b8ce1f22b557d1d7` | `e1c0135626bc7dfe950894a9e666df5d181d8c0d673023655221f27921445844` |
| binary SHA256/build_id | `a64232c11af4001495c11abe5e1d3380fe86b8e1f2eb0124ed90ef343fecd9fd` | `c8ad89711ce24997186bc06b7ffd4e812f1e3dfc4249c3622eacc23c5c30095f` |
| 完成时间（上海） | 2026-11-12 02:34:51 | 2026-11-12 03:16:06 |
| 状态 | full_snapshot_complete | full_snapshot_complete |

正式目录分别为`/mnt/nvme0/public/openalex-stats/results/core-goal-v1`、`/mnt/nvme0/public/openalex-stats/results/core-goal-v2`。两者均覆盖2,446个Works文件、510,372,821行，Q为269,194,661，输出年发表量201,267,475，保留引用边2,363,318,327。主cube 2,562,778行，statistics 14,966,202行，年度模型216个。这不是pilot数据。

v7复用已校验的v1紧凑维度，但在独立core-reference-v2/core-counts-v2目录重新生成全部2,446引用分片和64计数分区，edge_parts_reused/count_partitions_reused均0。source archive SHA256为`7edcd2e26e0e9b76b7b9e4da2e948fc4a80bef175ade2a9816443cd59461e333`。字典manifest及实际加载字典语义SHA均绑定身份。证据：[v4 run](../research/formal-run-v4.json)、[v7 run](../research/formal-run-v7.json)、[v7 completion](../research/formal-completion-v7.json)、[执行身份](../research/execution-v7.json)。计算状态不代替整个Goal验收。

## 2. 统计口径与质量

四层独立：Topics Field/Subfield、Concepts Level0/1，固定字典26/252/19/284。国家字典248个，原247个加实际出现的EH，扩展在unlisted_labels留痕，未合并或静默丢标签。

统一Q采用15类黑名单及is_paratext排除，主core；保留合格预2000目标和96,020条已撤稿原文。A→B两端均在Q，引用年取A发表年，国家/学科取B；每对去重、排同Work环、保留作者自引。六场景为国家whole/fractional×Concepts阈值0/.3/.5，同层Concepts去重取有效最大score后均分。UNKNOWN保留在cube/质量；RCA/ECI世界分母只含已知国家×已知学科矩阵。八套年度ECI/PCI只属于主dual-fractional场景，不给whole复制模型。

引用槽位精确闭合：`2,599,185,946 = 2,363,318,327保留 + 117,697,642目标缺失 + 117,216,603目标被Q排除 + 953,374同Work环`。另13,678,393条target年份晚于source的边保留并标记，占保留边约0.58%，未暗改日期。互斥excluded_reasons有优先级，不能将其中xpac 134,884,451解释为全部XPAC 192,552,631。

发表文献缺国家103,275,459条，已知国家覆盖仅**48.69%**；被引事件国家覆盖**88.32%**。这些文献仍在Q，国家曲线和ECI只反映可归属部分，不能声称全部国家产出覆盖。

| Concepts阈值 | Level0发表覆盖 | Level1发表覆盖 | Level0被引覆盖 | Level1被引覆盖 |
|---|---:|---:|---:|---:|
| 0 | 97.94% | 78.94% | 99.9977% | 98.91% |
| .3主 | 87.81% | 71.59% | 99.40% | 95.83% |
| .5 | 43.89% | 30.87% | 69.51% | 50.19% |

Topics发表分类覆盖约93.96%、被引99.93%。阈值提高造成分类UNKNOWN增加，不是Q或边减少。[方法/质量报告](methodology-and-quality.md)已解释这些局限，现已明确v1基线和最终v2同口径/等价关系及各自运行身份。

ECI使用正确对称相似变换、active索引、标准化及跨年共同国家方向。RCA在1附近1e-12舍入有模型字段和回归。零数据、断连、退化/不稳定不造分；正式216模型均ok。数值残差小不证明指标无测量偏差。

## 3. 独立验证链

[full-audit-v5](../research/full-audit-v5/audit.json)为passed、2,141项检查、failed_checks=0，核对全部normalized来源/输出hash、全量Q、年度引用总数、四层守恒及CN/US/GB×Field17/11/Concept Biology九组代表值。输入是normalized，不是全部raw第二次嵌套投影；独立代表cell主要是fractional/.3，不将其冒称六场景全部cell raw重建。

[跨版本SQL](../research/equivalence-v7/summary.json)passed：FULL JOIN逐cell比较全部cube 2,562,778、statistics 14,966,202、RCA长表29,932,404行，缺失/重复/标记/数值不符均0。容差abs1e-7+rel1e-10；发表差0、最大被引浮点差1.86e-9、RCA差约7.23e-11，大世界矩阵总量按相对容差判断。这是已审计基线到v7的完整输出语义桥接，不是新的全raw重建，不要求浮点文件字节相同。[SQL](../research/compare-v4-v7.sql)和源hash在摘要绑定。

[model-audit-v7-v1](../research/model-audit-v7-v1.json)检查v1数据；[model-audit-v7-v2](../research/model-audit-v7-v2.json)检查新v2数据。两者216/216、failed_models=0、failed_exports=0。从cube重建RCA/M并用一般非对称求解器验证算子、谱/残差、active索引、归一/PCI投影。方向包括8个首年和208个跨年共同国家协方差。新国家53,568行、学科31,374行长表与JSON逐标签/active/valid/status/score一致，最大score差0。

100k基线按无环规则已独立闭合：[Go](../research/regression-100k-v5.json)与[SQL](../research/pilot-oracle-no-loops.json)均为48,319条Q、193条边、388排除环，未沿用581条含环预期。

### 引用/整数target×year逐分区摘要

本次SSH只读两份约5.6MB引用manifest及128份小计数sidecar，[integer-artifact-equivalence-v7](../research/integer-artifact-equivalence-v7.json)passed：

- 2,446分片按source对齐，无缺失/重复；每片SHA256/Bytes、64维partition_counts、key、stats、scan全部相同。引用二进制总18,906,546,616 bytes，事件总2,363,318,327。
- 全64 target分区partition/Rows/Years/Sum/SHA256/Bytes/key相同；Rows269,194,661、Years27，总58,146,046,776 bytes、Sum2,363,318,327。不只比总和，覆盖每个分区完整序列摘要。
- 相同复用维度保证ordinal基准一致。在已提交writer hash和immutable假设下，支持每个target ordinal×year整数相同。

本次未打开或重hash约58GB计数、19GB引用或维度二进制；不能称为当前磁盘字节的独立重哈或raw语义oracle。原提交校验与immutable假设仍是证据链的一部分。报告记录元数据SHA与canonical tuple SHA，路径和完成时间不参与内容比较。

### raw→cache有界独立投影

[projection-audit](../research/projection-audit/audit.json)直接用DuckDB1.4.1读最早2016-06-24/part_0000的2,439行及最新2026-06-26/part_0006的400,000行，两个文件全部402,439行，threads2/2GB/13.96秒。23项不符均0：ID/year/type/flags、原始和trim国家集、primary Field/Subfield、Concepts0/1完整(id,score)多重集/每ID最大分、refs顺序/集合、列表及持久化quality字段。Go仅[编排](../scripts/projection-audit/main.go)，[SQL](../research/projection-audit/query.sql)直接读取raw，不调用生产scanner。

本地证据SHA已复核；normalized小分片实际SHA前后验证，raw绑定大小/mtime/指纹。没有重SHA读取全部raw快照。它关闭只有Go自我roundtrip缺口；两个有目的分区不代表全库逐行第二次投影验证，也不能估计全库错误率。

## 4. 性能与窗口独立复算

无重SQL竞争的[索引复测](../research/index-benchmark-20261112/followup-summary.json)包括5M真实ID三轮轮转和50M单轮尺度、510M向量32个分散窗口；普通后台仍存在。50M serial build14.3703秒，w16/s64 1.1485秒，w32/s256 .4842秒，w48/s512 .6189秒。w32构建29.68倍，但46并发lookup吞吐倒数10.23ns/op比serial9.929约慢3%，构建多约800MB临时分配。ns/op是aggregate吞吐倒数，不是单次延迟。全50M ordinal检查覆盖serial/w16/w48共150M；w32同算法另有回归，不冒称该case已做50M逐项核对。早期numactl缺失且未安装；后续使用进程级纯Go wrapper完成NUMA补测，见下节。

v7实际全量indices9.8658秒，基线406.1654秒；reference115.2816秒，基线111.6582秒。两阶段合计517.8236→125.1474，节省392.6762秒，包含lookup稍慢代价。这是不同真实运行观察，不是重复控制实验。v7内部576.19秒、外部约579.8秒；v4外部1165.91秒，但维度复用不同，不能把总耗时差当同条件纯加速比。

### 60秒平均通过，短时dip保留

独立结果为[performance-window-v7-independent](../research/performance-window-v7-independent.json)，未覆盖root原文件。[冻结source](../research/performance-window-v7-independent-source.json) SHA256 `49e59a518d2d14f2ebc15414ccacd7d2d9ffafe543eb2a25984c35992c75968c`。capture仍标running，但reference资源已完整；最终完成由正式run证明。按真实相邻elapsed差积分，不假设每样本1秒；精确60秒滑窗枚举边界及边界减60。

| 窗口口径 | reference阶段起点后秒 | CPU平均 | 整阶段GC全部保守扣除后 |
|---|---|---:|---:|
| root采样边界窗 | 21.002909367—81.003212609，60.000303242秒 | 93.2654294% | 93.0879164% |
| 独立最优精确60秒 | 22.006436723—82.006436723 | 93.2770630% | 93.0995491% |

root数值正确。GC扣除把整个115秒阶段全部5.1124 GC CPU秒都计入60秒窗，除以48线程×时长，是保守全扣。项目user+system作分子，整机48线程作分母，不含其他进程/iowait。

**窗内6个采样区间低于90%，合计5.9981秒，最低78.1995%；每个区间均≥90%的最长连续序列46.0186秒。**支持通常按持续窗口平均解释的60秒目标；不支持整分钟每秒≥90%。如果将Goal解释为严格逐秒门槛，该版本未达，不能隐藏。trace无每区间原始/proc/stat ticks，独立积分以已发布百分比和实际时长重建；阶段报告另按累计ticks加权，存在量化/时间戳差别。

### 其余资源与未达阶段

reference阶段均值92.0124%；全程29.47%，不是90%。counts32workers68.51秒/22.83%（基线24workers76.99秒）、aggregate60.31秒/56.59%、derive37.33秒/4.45%、export41.56秒/2.08%，仍有低CPU阶段。已定位的profile/布局/并发对照和真实收益可认可，不声称普遍最优。

峰RSS134.53GiB、最低MemAvailable592.14GiB，满足640GiB预算和64GiB余量；process swap峰0，全机swap-in10页/40KiB、swap-out0。全机计数不归因本进程，未见持续新增swap I/O。reference逻辑读19.26GiB/物理读0，是热文件系统缓存，不声称冷读。全程累计分配361GB不是同时RSS；GC117次、CPU32.23秒、STW .0821秒。具体IO/GC/heap见[v7 run](../research/formal-run-v7.json)和[性能报告](performance-report.md)。没有为RAM90%制造无用分配。

串行SHA瓶颈现已完成四模式全cache验证对照：serial94.3481秒、8workers13.3621秒、16workers6.8332秒、24workers5.0790秒，24较serial18.576倍。各模式完整2446文件/510372821行/31900483518bytes，ordered manifest SHA一致，所有SHA及source stat检查保留。每模式单轮且physical read0，为热缓存观察，不是完整统计5秒重跑。证据：[cache benchmark](../research/cache-validation-benchmark-20261112/summary.json)。v8只修改输入校验并发和reader默认，依赖不变、统计kernel不变，见[三文件runtime差异](../research/release-v8-code-diff.json)。本次读取实现确认每片完整SHA、source大小/mtime、输入顺序、取消和worker预算仍在；v8 race所有package通过。NUMA两轮候选对照、热缓存和低CPU阶段的适用范围仍保留为边界。


### 批次候选实测的补充验收

root最终逐字核对Goal第四节发现批次候选虽已实现但缺少独立实验记录，已补[batch benchmark](../research/batch-benchmark-20261112/summary.json)和[范围说明](../research/batch-benchmark-20261112/README.md)。本审阅检查全部12份原始run JSON、资源记录及独立副本patch，并重新计算中位数：

| 批次行数 | 3轮中位秒数 | 最大RSS MiB |
|---|---:|---:|
| 32 | 14.043972447 | 48.97 |
| 128 | 18.930307352 | 51.59 |
| 512 | 15.013936138 | 62.84 |
| 2048 | 13.380947629 | 96.97 |

4个真实normalized文件、1,434,610行/10,853,745参考条目/90,342,117压缩字节，四reader、GOMAXPROCS46。全部12次完整有序内容SHA均为`2e04815e8704a98874dffe8b56d979aa5d544d2d400a7eba3266f316553ba7b2`，rows/refs/countries/concepts/fields/subfields及完整quality Stats完全同一。input SHA记录与四分片manifest相同；本次没有再次读取远端Parquet。每次资源exit0、physical read delta0，属于热缓存实验。

独立复算candidate源码和patch SHA、helper源码SHA均匹配，baseline scan源码与当前生产文件逐字节同一；当前三处v8 runtime hash仍匹配发布记录。生产默认128及v8程序未改。2048中位比128耗时下降29.32%、该实验吞吐增41.47%；相对32只快4.72%，RSS约翻倍。

该实验包括完整字段解码/Work构造、四reader有序回调和canonical全内容SHA，回调可能串行化，CPU约1.2—1.4逻辑核；**不是生产逐文件独立refs-only扫描的全流程对照**，不把41.47%外推成production加速，不声称默认128为全局最优或实验达到90%整机CPU。它关闭Goal明文批次候选实测的缺证，不改变此前23项检查和正式全量CPU证据。已新增第24项签核check；未重开泛化优化。


### NUMA候选实测的补充验收

早期numactl不存在是命令缺失，不能据此认定内核策略不可设置。现由[NUMA实验](../research/numa-benchmark-20261112/summary.json)补齐50M真实ID、w32/s256、GOMAXPROCS46的default/interleave各两轮对照。纯Go wrapper锁定OS线程并调用SYS_SET_MEMPOLICY后exec，未安装工具或改全局/生产策略。本审阅核对8个build/lookup子进程：GET_MEMPOLICY分别为mode/mask 0/0与3/3、errno0；所有实际Name=graph.test的numa_maps均为default或interleave:0-1，逐snapshot页分布及最大采样摘要与报告相同。退出0、swap0、filesystem input0；wrapper/launcher源码SHA及现有production runtime SHA独立重算匹配。

| 策略/轮次 | 构建秒 | 串行lookup ns/op | 并发lookup ns/op |
|---|---:|---:|---:|
| default 1 | .544700967 | 361.3 | 10.040 |
| default 2 | .545111732 | 294.1 | 9.519 |
| interleave 1 | .569925302 | 282.0 | 9.288 |
| interleave 2 | .750926916 | 283.5 | 9.330 |

原始benchmark stdout与以上summary数值逐项相同；两个策略复用同一记录SHA的graph.test及同一all-ids输入。interleave构建两轮更慢、lookup略快且波动明显，未证明全流程收益或全局最优。构建仅1次计时，lookup按1秒校准；CPU/RSS是含样本加载/校准的完整子进程资源，不是阶段90%CPU证据，并发ns/op仍是吞吐倒数。生产默认/源码保持不变。Goal明文NUMA候选实测的缺证关闭，新增第25项check，其余24项不变；不再把NUMA未测作为当前限制，也不追加泛化优化待办。

## 5. 正式重放、配置与服务

[replay-v7-validation](../research/replay-v7-validation.json)passed：同v7 binary/CLI配置/输出目录返回同run_id、原StartedAt/CompletedAt/source/Q/edges。completion SHA `77569da27845ff1822c9cd7b4096652444b6613742460f8bbf8eb9ab01819584`不变；仅cache/dictionary验证，无维度/index/reference/count/aggregate/export重算。正式幂等证据缺口已关闭。源cache中断后真实复用570分片另见[prepare](../research/full-prepare-run.json)。

配置缺口已修：Config.Scenarios、--scenarios、ResolveScenarios验证唯一ID、合法模式、有限[0,1]阈值和唯一主dual-fractional；.2/.4 Dense/map对照、动态owner和错误owner拒绝测试通过。新ECI JSON显式scenario_id，默认Parquet数值语义未改，但JSON/hash变化可解释。字典manifest/semantic SHA入run fingerprint，防字典改变错误复用；有回归。未改写旧v4不可变产物。

[browser-formal-v6](../research/browser-formal-v6/browser-qa.json)24/24通过、errors为空、上海02:53:23完成，绑定v1正式数据。覆盖四层×两输入8项、真实多国曲线、当年被引、ECI学科无关、PCI单全球曲线、whole不借模型、partial、方法/下载/路径安全、桌面/移动绘制值与API/表格一致及无外部请求。此处只审阅既有记录，不冒称本审阅者重跑浏览器。新增[正式v8 QA](../research/browser-formal-v8/browser-qa.json)于上海03:33:32完成，status passed、24项全true、errors0、外部请求0；[实际服务身份](../research/browser-formal-v8/service-identity.json)核对PID877825、v8 binary SHA744b93484e7b56fb0099bd04515c145a55ef9527aecbea93fc5a39dac6db682e、core-goal-v2及run e1c013...，run_id来自实际下载run.json。真实曲线/partial/下载及[桌面移动视觉复核](../research/browser-formal-v8/visual-review.json)通过，不是给旧v1 QA换名。启动前导航失败单独保存startup-attempt，最终记录为监听后重试，不隐藏初次未就绪。

## 6. Goal逐项证据清单

每行通过只指其证据范围，不代表整体Goal完成。v7正式结果已由全cell、模型和整数摘要桥接验证；新代码、最终服务与文档仍需对应实际身份。

| Goal要求 | 证据与判断 | 边界/剩余 |
|---|---|---|
| 一：复用项目/229重探/源只读 | README、工程方案、server-capacity、execution-v4/v7 | 通过；delivery/README/manifest已绑定v7统计与v8服务 |
| 二.1：2000起/完整年/2026partial | 两run、methodology、browser24项 | v1/v8-v2 UI24项均通过 |
| 二.2：两规范四层、无父子混合 | 字典26/252/19/284、216槽位、模型审计 | v7通过 |
| 二.3：15黑名单/paratext、缺失新类型保留 | inventory/policy、raw flags/type、Q守恒 | 通过；raw独立仅两文件 |
| 二.4：Q双端/施引年/target归属/不提前缩施引 | 全量SQL、九组代表值、手算；v7全部cell/整数摘要 | 通过；非全raw重建 |
| 二.5：pair去重/排Work环/保作者自引与撤稿原文 | 图统计、953374环/96020原文、独立100k193边 | 通过 |
| 二.6：core/旧目标/原代码/双权重/primary/Concept分摊阈值 | 六场景、字典EH、Dense/map、全cell等价 | 通过 |
| 二.7：XPAC与所有口径配置化、methodology | inventory、Config/policy、--scenarios、dict fingerprint | 已实现/测试，README及方法文档已统一 |
| 二.8：8套模型/RCA分母/归一方向/无效状态 | v2独立216+两长表，8首年/208跨年 | 通过；无效分支fixture，本次正式均ok |
| 二.9：国家ECI全结构、选学科指标与全球PCI | query定义、browser24项 | v8-v2已复核 |
| 三：Go主实现/subagent并发集成/固定依赖 | scan、graph/checkpoint、metrics、web/audit | 已实现，有源码/binary身份及归档 |
| 三：工具替换需同语义基准 | Go生产、DuckDB oracle、5M三轮/50M/全量索引 | 无无证据换主语言；SQL仅oracle |
| 三：缓存/CSR/target整数/取消背压/日志pprof | cache31.9GB、紧凑store/dense、分桶检查点/测试 | 已实现，不逐边展开国别学科 |
| 三：队头阻塞/并发乘法/完整内存估计 | 分文件独立调度/46预算、估算与实测 | 有实现和资源证据，不用goroutine数替CPU |
| 三：run_id绑定输入配置源码依赖/恢复/Q变化失效 | keys/hash、dictionary manifest/semantic、replay | 通过已查范围，正式无重复累计 |
| 三：专属目录/只读源/不停服务不改全局不上公网 | 执行/guard/LAN记录、无外部请求 | 审阅未见越界 |
| 四：资源重探46起点/worker reader batch layout NUMA调优 | reader46/16观察、四批次×三轮、索引复测、counts profile/32workers | reader非控制实验；NUMA两轮实测未证明全流程/全局最优 |
| 四：代表性60秒有效CPU≥90% | 独立93.2654%，全阶段GC全扣93.0879% | 窗口平均达标；6dip/最低78.20%，严格逐秒未达 |
| 四：分阶段CPU/RSS/heap/GC/IO/队列吞吐 | v7 trace/性能报告、无缓冲队列/46worker/未完分片日志 | 已记录；v4缺字段不倒填 |
| 四：640GiB/64GiB余量/swap/磁盘 | RSS134.53GiB/可用592.14GiB/process swap0/host10页入0出 | 安全余量有证据，不制造RAM90% |
| 四：低CPU持续优化/实测限制 | 索引+图省392.68秒、counts68.51、profile/布局对照 | derive/export仍低；cache四模式全量对照完成 |
| 五.1：项目/依赖/CLI配置/运行恢复文档/关键测试 | README、configs、test/vet/race-v7、归档 | CLI/服务/恢复说明与发布身份已核对 |
| 五.2：完整manifest正式计算不冒抽样 | v7 2446/510372821、max_rows0、completion | 已通过 |
| 五.3：四层双权重/份额RCA/Parquet字典 | 14.97M stats/29.93M RCA/全cell/valid语义 | 已通过，不给未观测RCA造值 |
| 五.4：8套年度ECI/PCI/诊断方法有效标签 | v2 216模型/53568国家/31374学科独立审计 | 已通过 |
| 五.5：类型XPAC/未知坏ID/无法连引/重复环/异常覆盖质量 | inventory/graph/coverage/raw23项/槽位精确闭合 | 已通过；48.69%等覆盖局限保留 |
| 五.6：manifest版本/hash/分片性能恢复 | run/completion/execution、全cell/整数摘要/replay | 已通过范围；新代码不可冒v7身份 |
| 五.7：229真实曲线/切换多国指标/partial方法下载 | LAN8899/v1 browser24项 | v8实际v2服务24项QA通过 |
| 六：test/vet及229 race | race-v7所有package/既有test/vet | v8 Linux race通过；本地test/vet已由root确认 |
| 六：手算真实分组全量守恒/容差整数/恢复无重复 | full-audit/100k193/模型/全cell/整数摘要/replay | 通过；摘要非本次大二进制重哈 |
| 六：真实展示/独立完成审查/状态持续维护 | browser24、本文件、goal-progress、方法性能 | 最终入口/文档已核对；签核无阻塞 |

## 7. 最终独立签核

[机器可读独立签核](../research/independent-final-acceptance.json)为accepted_with_explicit_limits，25项检查通过、blocking_findings为空。补充核对包括：

- 本地实际重hash v8二进制、frozen v7二进制/源码归档以及当时4.94MB发布包，均与[release manifest](../research/release-v8.json)匹配；正式completion实际SHA匹配，11个统计artifact记录逐项与完成清单相同，实际服务下载run.json匹配完成清单SHA。
- 从frozen v7源码归档与当前49个runtime Go文件独立比较，只有compute reader默认、run调用validator及新增parallel validator三文件变化；go.mod/go.sum逐字节不变。已有两个文件的diff仅为reader预算和validator入口；新增实现保留完整SHA、source stat、顺序、取消与全局worker限制。
- v8 browser九个证据文件本地SHA/Bytes重新核对全同；24检查均通过，实际服务PID877825、v8二进制和v2数据/run绑定。四模式完整cache基准独立复算18.5762059倍，单轮热缓存边界正确。
- README、delivery、方法、性能和goal-progress的主要数值/命令/身份一致；先前ECI特征向量措辞和GC秒数误述已修正。方法已补v2身份，旧pending不再作为假待办。

本地test/vet依据root实际执行通过与release记录，未由本审阅者重复运行；远端v8 race完整日志已读取。本次不重扫大型统计文件，沿用绑定hash的已执行全量数值审计与正式重放；整数摘要不冒称新磁盘重哈。

最终发布包需把本签核纳入，因此root将在签核后重打archive并独立验证，**只更新manifest.source_bundle的bytes/SHA及accepted状态**；这是明确交接的打包动作，不改变已审runtime、统计数据或语义。本文件/JSON不递归绑定未来最终包SHA；最终实际包身份以刷新后release manifest为准。

在通常60秒窗口平均口径下，代表性CPU目标已通过，低CPU阶段、6个dip/严格逐秒未达、热缓存/维度复用、NUMA仅两轮且未证全局最优、国家/分类覆盖及raw验证范围均已披露。没有把例外藏为未说明待办，也不据旧pending拖延签核。root完成上述最后归档hash核对后，可据此作Goal完成判断。