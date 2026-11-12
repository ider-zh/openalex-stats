# 重现本次外部元数据试验

本批整体/净域均失败，实际外部新增0。不要把候选当可应用补丁；不要在本批Gold之后改阈值、合并两个128或扩样。

依赖：Go 1.26.0 linux/amd64、parquet-go v0.32.0（根go.mod/go.sum）、Python3.13.12/DuckDB1.4.4。Windows只承担公开文件运输：Node24.3.0/undici EnvHttpProxyAgent及curl8.16.0；没有Country统计模型API、Crossref/PubMed逐文献API或API key。

1. 按source-v1.json准备固定源，复核全部校验值；PubMed是完整January2026 baseline，不含updates。不要将未完成来源当不存在。
2. 使用原Q/UNKNOWN thin manifest，运行build_work_index.py生成权威列投影，每行核原ID/year/type/XPAC。原始快照/cache不改。
3. 编译Go source pools及policy-engine；先跑core、pubmed-pool、work-index测试。解析源并冻结源码/别名/源文件/执行binary SHA，之后才释放独立新样本。
4. prepare_matches.py只做精确标识符发现。build_author_index.py一次合并metadata/PMC作者targets；后续--reuse-manifest必须用新的--out版本，保留第一版。
5. evaluate_matches.py调用冻结Go binary，保留所有raw outcomes。personal_country_scope.py只生成角色证据差分与whole-Work quarantine，不改原国家集合或身份输入；test_personal_scope.py使用合成数据做边界QA。
6. 独立validator冻结整体/净域论文簇样本，extract_selected_pubmed.py和blind包工具只提供原source bytes/原Work，不把预测交Gold reviewer。核完全部Gold SHA后才比较预测。
7. 两域通过wrapper、冻结global-defer名单和证据SHA齐全后才允许生成正式旁路国家patch。本批未满足，因此没有可应用外部patch。

脚本默认路径对应本次229服务器布局。历史binary SHA9f17ef...用于确认本批实际执行文件；在不同路径/工具链重编译可能改变binary字节，必须建立新的执行冻结记录和新的验证运行，不能把新binary或新规则冒称旧Gold已验证。多worker源Parquet的分区/证据顺序可能改变字节SHA；数学/成员重现应按WorkID、规范国家集合及稳定源行键比较，同时为每次实际artifact重新冻结manifest。

公开只含显式白名单内源码、规则/config和无原文纯汇总。原作者、署名断言、XML/PDF/HTML、原inputs/outcomes、盲包、Gold与country支持原文不得加入Git。完整原始证据仍在私有研究目录供用户review。

公开包不包含原机器的 `bin/proxy-fetch.mjs` 或其 `bin/cloudflare-runtime/node_modules/undici`。运输脚本使用全局 fetch；直连环境可直接运行，需要代理的环境须自行配置 Node／undici 的启动代理，或使用既有 curl 运输路线，并重新记录传输工具版本。此运输依赖不改变已冻结的解析与国家准入规则。
