"""Report observed operational denominators; candidate yield is not a passed patch."""
import json,pathlib,collections,hashlib,os
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/fulltext')
pdf=root/'pdf-probability';transport=json.loads((pdf/'transport-results.json').read_text());records=transport['records'];outcomes=[json.loads(p.read_text()) for p in (pdf/'policy-results').glob('W*.json')]
stop=json.loads((root/'grobid-stop-receipt.json').read_text());stat=stop['stat'].rsplit(') ',1)[1].split();hz=os.sysconf('SC_CLK_TCK')
summary=dict(status='completed_probability_trial_no_pdf_rollout',policy_sha256='09312f219af3980328199a6cad05386a71ad8b957e505cb847ae6eefae1c28a1',frozen_plan_sha256=transport['plan_sha256'],selected=len(records),selection_fixed_before_source_outcomes=True,transport_status_counts=dict(collections.Counter(r['status'] for r in records)),frozen_outcome_status_counts=dict(collections.Counter(r['status'] for r in outcomes)),transport_elapsed_serial_seconds=sum(r['seconds'] for r in records),transport_elapsed_max_seconds=max(r['seconds'] for r in records),downloaded_response_bytes=sum(r.get('bytes',0) for r in records),real_pdf_bytes=sum(r.get('bytes',0) for r in records if r['status']=='pdf_reached'),grobid_process_cpu_seconds=(int(stat[11])+int(stat[12]))/hz,grobid_stop_status=stop['status'],consolidation_enabled=False,crossref_pubmed_api_calls=0,source_concurrency=3,independent_precision_gate='insufficient_n',minimum_independent_positive_candidates=128,automatic_candidates=sum(r['status']=='automatic_country_candidate' for r in outcomes),unobserved_eligible_pdfs_not_treated_as_failures=True,production_applied=False)
(root/'pdf-cost-summary.json').write_text(json.dumps(summary,indent=2)+'\n')
md='''# 开放 PDF 分层留出试验（冻结规则）

这一路线完成既定 383 条公共 PDF 地址请求，不扩大样本。获得 109 份有效 PDF，严格文献身份与原作者一致性规则最终准入 5 篇。单独精度门槛要求至少 128 个独立准入样本，因此结果为 **insufficient_n**，不能推广成全量补全规则，也没有应用国家补丁。

| 环节 | 数量 |
|---|---:|
| 预先抽取 URL | 383 |
| 有效 PDF | 109 |
| HTML 或非 PDF | 122 |
| HTTP 失败 | 101 |
| 传输失败 | 51 |
| 严格规则自动准入 | 5 |
| 文献身份/版次/原作者不满足规则 | 95 |
| 有效解析但无可支持作者国家 | 2 |
| GROBID 解析失败 | 7 |

验证 agent 逐一完成 5 份原始 PDF 盲审，先读原文建立 gold，再揭示预测：5/5 的文献身份和预测作者国家均有原文支持；可见作者国家集合完整为 4/5，全文国家集合完整性仍为 unknown。5 个成功个案低于 128 的独立准入审查门槛，因此精度结论仍为 insufficient_n，不能推广。

所有失败保留原分母；没有绕过访问限制、扩大抽样凑数或使用人工个案覆盖自动规则。源文件、版本与作者证据保留。GROBID 仅监听 loopback，所有 consolidation 为 0，完成后已精确识别并停止本次 JVM，两个 loopback 端口均已关闭。源对象不写入原始 snapshot/cache，也不改变当前界面数据。

后续以结构化 PMC 路线为优先，PDF 大范围应用缺乏当前质量门槛所需证据。自动准入不足不能解释为已证明其准确率为 100%。
'''
(root/'pdf-trial-report.md').write_text(md)
print(json.dumps({k:summary[k] for k in ['selected','automatic_candidates','downloaded_response_bytes','grobid_process_cpu_seconds','independent_precision_gate']}))
