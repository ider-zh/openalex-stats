# 小倪方法论：网站名称与默认选择

2026-11-12 已发布至 229 网站。代码提交 `8ae9a81`（GitHub main），前置数值稳定修复为 `64fe632`。Cloudflare 构建与 229 Go 服务保持相同选择逻辑。

- 统一方法按钮、tooltip、导引和 API 展示名为“小倪方法论”。
- 未指定方法时，国家 ECI 和学科 PCI 默认使用小倪方法论；显式谱方法分享链接及用户切换仍生效。
- 保留内部方法 ID `java-reflections-v1`，国家 16 轮／学科 17 轮定义、原统计与模型文件保持不变。
- 只有谱方法产物的数据集继续使用其可用方法；数量／被引图原始默认指标不变。
- 模型下载链接显式携带当前方法，切回谱方法后下载对应谱模型。

验证：Go `internal/web`、Cloudflare 8 项单测通过；最终构建 7,399 文件、196,061,071 bytes。Pages 预览、229 隔离服务、229 正式服务的双语料 12 项浏览器检查均通过，逐点值／全球排名与原模型精确匹配，覆盖显式链接、ECI／PCI、CSV、下载及 390px 移动端。

正式服务：`http://192.168.1.229:8899`，PID `2622389`。二进制 `oa-stats-xiaoni-default-v2-linux` SHA256：`4c218be10e5df06a1cc2c5f0506e78036c198851d24807fea5d10101472d1c96`。原二进制与版本保留；隔离候选进程已按完整 PID／命令／启动时间校验后停止。

两版 run ID 保持原值：Core `4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455`；All `dd679f119b4d24a9d6725e79dec96af9d731265de59844ae04bbd028a719153e`。国家补全旁路数据没有应用于此次网站更新。

私有 QA：`research/ui-xiaoni-default-v1/{pages-r1,go-candidate-r1,production}/method-qa.json`；服务器发布证据：`/home/ider/tmpdata/openalex-stats-dev-20261112/ui-xiaoni-default-v1/`。
