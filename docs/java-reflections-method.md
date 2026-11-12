# Java 反射法·修正版

本方法在当前已审计统计数据上复现旧 Java 常规单矩阵反射法的数学路线。实现仍用 Go；不运行旧 Java 应用或连接旧数据库。原谱方法与其完成清单完整保留。

## 相同输入

core、all 两个语料各使用原 `eci.json` 的精确 RCA、二值优势矩阵 M、国家/学科词典及有效索引。输入来源绑定原 run ID、completion 和模型文件 SHA256；每个模型另有网络 SHA256。论文量、引用量、份额、RCA 和文献集合不由方法 switch 改写。

模型仍为 Topics Field/Subfield、Concepts Level0/1，分别按发表量和当年被引建模，2000–2026 共 216 个模型/语料。2026 为部分年。国家 ECI 使用该层全部学科；PCI 是学科全球值。默认主场景仍为国家均分 × Concepts 命中全计数，score ≥ 0.3。

## 保留的反射迭代

令 `kc0=M·1`、`kp0=Mᵀ·1`，分别表示国家的优势学科数与学科的优势国家数；并令 Dc、Dp 为这两个度数的对角矩阵：

```
kc(n) = Dc⁻¹ M  kp(n−1)
kp(n) = Dp⁻¹ Mᵀ kc(n−1)
```

两侧都读取上一轮。默认国家取第 16 轮，学科取第 17 轮；后者是前者经学科度数归一化的投影。最终国家和学科向量分别作总体 z-score。结果是有限次反射指标，没有使用谱方法分数作为初值或替代输出。

16/17 为本次明确设定，不能据此宣称恢复了同事历史运行参数。敏感性报告同时评估 12/13、16/17、20/21，与当前谱结果比较平均并列秩的 Spearman 相关及前十重叠。

**已观察到显著方法差异，不能把两方法宣称为相同排名的替代实现。** 默认 16/17 时，core/all 的 Concepts Level0 × citations 在 27 个年份均出现负的跨方法国家秩相关（约 −0.98 至 −1）。其它多数层级/输入组合高度正相关，但 core 的 Concepts Level0 × publications 也存在秩相关约 0.33 的年份。

例如 core 的 2000 年 Concepts Level0 × citations：谱分数与多样性的协方差约 4.9173，反射分数与多样性的协方差约 21.1528，两方法国家分数之间协方差约 −222.9149。二者均满足首年正多样性相关的规则；共享网络与独立两步迭代检查也通过。有限迭代仍混合多个模态，不能把所有显著差异解释为实现错误或任意翻号。这里没有额外把反射分数翻成与谱分数正相关；那会改变已声明的独立定向规则。界面和下载应保留这些差异供研究 review。

## 修复与诊断

- 旧代码标为标准差的中心化 L2 范数，改为总体标准差。
- 删除 Harvard/Population 单参照对象停止规则；不再因未找到对象而回退初始度数。
- 固定国家偶数轮、学科后续奇数轮；校验参数及网络有效索引。
- 断连/不可识别的共享网络不生成全局分数；有限反射本身零方差也显示无效。
- 拒绝非有限值，且不把接近常数时的浮点噪声放大为排名：标准差须超过当前数值尺度的 32 个 ULP，并满足标准化下限。
- 首个有效年按国家多样性相关性定向，后续沿用现有共同国家协方差定向，ECI/PCI 同时翻转。

`result.residual` 是反射向量相对原网络第二模态的归一化参考残差，定义为 `||center(Tz)−λ₂z||/||z||`；有限反射法不要求它为零。该诊断不会把有限反射分数改成特征向量。

## 正式结果与验证

| 语料 | 原始统计 run | 新方法旁路目录 |
|---|---|---|
| core | `4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455` | `/mnt/nvme0/public/openalex-stats/results/core-java-reflections-v1` |
| all | `dd679f119b4d24a9d6725e79dec96af9d731265de59844ae04bbd028a719153e` | `/mnt/nvme0/public/openalex-stats/results/all-java-reflections-v1` |

每个语料 216/216 模型均有效，通过精确网络哈希、独立两步算子 `T^(n/2)kc0`、人口标准化及年度定向校验。独立计算顺序与交替迭代之间的最大浮点分数差为 core `7.69e−8`、all `9.94e−8` 以下，满足按信号 ULP 尺度计算的审计容差；无低精度模型。浮点两步运算顺序的差异不能称为数学上不同的算法。三套轮数都参与独立审计。

独立 DuckDB 逐行对照模型 JSON 与两种 Parquet 导出：每个语料 53,568 条国家记录和 31,374 条学科记录全部闭合，缺失/多余、状态/active/valid/unstable 差异和分值差异均为零。查询和结果保存在同一证据目录。

小矩阵测试与此前静态翻译的 Java 第 16 轮算术一致，并验证第 17 轮 PCI 投影、有限轮数差异、原谱结果不变、常数初值不伪造分数、非法参数及篡改输入拒绝。Linux 下 eci/methods/pipeline 的 race 检查通过。

旁路文件包含完整模型 JSON、国家 ECI 和学科 PCI Parquet、模型诊断、默认轮数审计、敏感性报告、manifest 和最后原子发布的 completion。`source` 身份复核覆盖 manifest 中列出的输入文件；整个 derivation 不以写模式打开原统计文件。

当前计算可执行文件 `oa-methods-v1` SHA256：`75f83f27d94ab801fdd0f33edd0235a11753a110a3ba5f4d559afffa209a5a64`。
冻结源码 `source-methods-v1.tar.gz` SHA256：`78b24cc680a1144146f637dab3b1972a89b3f652dcd7eefc4d40952c4a83b1c0`。

本地证据：`research/java-reflections-v1/{core,all}/{manifest,completion,audit,sensitivity}.json`。

## 复算与展示

```sh
oa-stats derive-method --data /path/to/completed-core --output /path/to/new-core-method --workers 8
oa-stats derive-method --data /path/to/completed-all --output /path/to/new-all-method --workers 8
```

输出必须是原统计目录之外的全新目录，不覆盖已完成方法目录。服务通过独立 `--methods-core`、`--methods-all` 参数装载旁路；部署验收以最终界面发布记录为准。

方法 ID 为 `spectral-v1` 和 `java-reflections-v1`。前端方法切换只作用于 ECI/PCI，保留语料、年份、国家、学科与发表/被引基础。ECI 不因多学科选择复制，PCI 不因多国家选择复制；Bump 的全局排名按各方法的完整有效模型计算。旧链接缺少方法参数时仍默认谱方法。

本版本没有把旧数据筛选、作者角色单国归属、五年滚动窗口、其它 RCA 分母或联盟双网络混入同一 switch。那些是独立研究口径；详见 [Java 方法差异](java-method-diff.md)。
