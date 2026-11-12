# 首次使用导引

页面首次成功读取真实统计结果、绘制图表后，展示六步使用导引。完成、跳过、关闭或按 Esc 后，距离该操作满 **7 × 24 小时**再次自动展示。普通访问只读取记录，不延长期限。页首“使用导引”可以随时重看，手动重看结束后重新计算七天。

导引不会修改国家、学科、方法、URL 或曲线数据。当前指标未显示方法控制时，说明其入口，不自动切换指标。导引使用非模态对话框，保留页面操作与滚动；支持上一步、下一步、键盘焦点和 Esc。所有样式、脚本均为本地资源，无外部追踪。

存储键为 `openalex-atlas:onboarding:v1`，记录格式为 `{ "version": 1, "dismissedAt": 1794441600000 }`（示例时间）。只在导引结束时写入；未完成而刷新页面会再次展示。同一页面会话最多自动启动一次，存储被禁用也不会反复弹出。损坏、错误版本或未来时间视为无有效完成记录，下一次正常结束时修复。未来文案调整应保留 v1 键与 schema；如确需迁移应读取旧记录的有效时间，避免仅因发布版本变化重新提示。

集成：引入 `/static/onboarding.css`，按 `bump.js`、`onboarding.js`、`app.js` 顺序 defer 加载。应用第一次成功绘图后设置 `workspace.dataset.ready = 'true'` 并向 document 派发 `openalex:ready`；模块会创建页首帮助入口并按七天规则启动。`window.OpenAlexOnboarding.start()` 手动重看；`maybeStart()` 判定后启动；`mount()` 仅挂载帮助入口。失败或未就绪页面不会自动遮挡重试。

验证：`node --test scripts/onboarding-test.mjs`；`QA_URL=http://候选服务 node scripts/onboarding-qa.mjs`（PowerShell 先设置 `$env:QA_URL`）。浏览器验收默认使用真实服务，不注入模拟统计数据。
