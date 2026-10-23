# 研究平台产品复核与具身智能工作流

日期：2026-10-21，Asia/Shanghai。本轮沿用现有工作区和数据库，不覆盖已有未提交成果。此报告分别记录源码/测试结果、真实运行结果和仍需外部凭据的部分。

**后续更新**：用户提供 DeepSeek 凭据后，已保留 Kimi 配置并完成真实模型写作、两轮修订、历史记录、自动学习和独立图片理解续验。最新结果见 [DeepSeek 接入与写作验收](RESEARCH_DEEPSEEK_ACCEPTANCE_2026-10-21.md)；下文保留首轮 Kimi 401 与人工验收的历史事实。

## 交付范围

- 修正研究工作台的数据分区：收件箱固定为个人收集入口；我的资料只显示主题资料集；团队项目按授权共享库显示。删除重复下拉和重复项，修复 URL 恢复、未保存正文保护和权限入口。
- 修复研究稿四个弹窗的可访问关闭按钮与 Escape 关闭，保留请求执行期间的保护。
- 用暖纸色、深墨绿、阅读排版和克制的半透明导航统一视觉；补充按钮悬停/按压、键盘焦点、深浅主题、触屏及减少运动偏好。
- 补全无索引资料集的笔记发布和状态文案，不再把没有摘要/Embedding 模型一概当成不能保存正文。
- 图片提供明确的「仅保存原图」路径。原件和来源版本保留，状态为 `archived`；不识图、不生成文字分块，不能直接作为研究稿文字证据。配置视觉模型后可明确重新识别。
- 修复写作学习成功重试的来源权限检查，以及修改开关/模型后旧自动任务仍继续运行的问题。
- Langfuse 从 Compose 固定关闭改为可由环境显式开启，默认仍关闭；不新增服务。

完整交互盘点与五步法取舍见 [冗余与可用性审计](RESEARCH_USABILITY_AUDIT.md)，学习、授权、队列与迁移边界见 [兼容性审计](RESEARCH_COMPATIBILITY_AUDIT.md)。2563 个交互节点的静态清单不等于每个控件均经过真实浏览器操作。

## Kimi 配置与实际阻塞

用户给出的控制台地址是 Kimi Code 官方入口。Code 接口为 `https://api.kimi.com/coding/v1`，K3 的调用 ID 为 `k3`；开放平台的 `kimi-k3` 使用另一套平台凭据与接口。依据：[Kimi 官方模型配置](https://www.kimi.com/help/kimi-code/models)。

截图凭据仅在本机私密路径处理，没有写进源码、报告或构建上下文；平台配置保存后，临时明文凭据文件已删除。对官方模型列表和真实 Chat Completions 的请求均得到 **HTTP 401 / invalid_authentication_error**；复核截图中易混淆字符后仍未通过。不能据此宣称 K3 已接通，也不能把人工验收稿称为模型生成。

已在当前验收账户（`ui-qa@example.invalid`）保存对话及视觉两个 `k3` 配置，显示名分别为「Kimi K3 · 凭据待验证」和「Kimi K3 视觉 · 凭据待验证」。配置使用正确官方接口；平台模型测试也实际得到同样的 401。模型配置已保存不代表连通性通过，未将其设为自动学习模型。安全元数据在 `artifacts/research/product-audit/kimi-models.json`。兼容参数回读与 10 项离线请求构造检查通过：研究默认选项不关闭推理，high/max 使用准确字段，不发送不支持的温度参数。离线检查不等于上游推理成功。

待有效凭据后继续：真实初稿生成、两轮模型修订、从修改自动提炼候选、确认后复用及反馈、独立图片识别。Kimi 聊天/视觉能力不代替 Embedding；本轮资料集关闭向量和关键词索引，选定正文写作与整库语义检索是不同能力。

## 真实材料和工作流

资料集：`9ca4d280-c5ec-4f77-a139-77172a2b61d9`。测试材料为公开的历史研究材料，用来验证流程，不宣称代表 2026 年最新研究进展。

| 材料 | 已有证据 | 实际边界 |
| --- | --- | --- |
| π₀ 论文 PDF | 原件与历史原件 SHA-256 一致；129 个正文块；归档正文与 12 张提取图片可授权回读 | 图片保存不等于模型理解；未测试扫描 PDF OCR |
| DigitalBuddy 官方文章 HTML 快照 | 原件与历史原件 SHA-256 一致；23 个正文块；正文和提取图像可回读 | 明确保存自官方网页，是其公众号文章的官方替代来源；部分提取图像为页面装饰，不能声称已归档文章全部插图 |
| 公众号直链 | 重试成功：22 个正文块、8,335 字归档正文，网页快照可读取；核对正文特征，非验证码页面 | 仍有 24 个外链图片未离线归档；官方镜像另有完整归档图片路径 |
| π₀ 官方网页 | 重试成功：27 个正文块、13,467 字归档正文及网页快照 | 验证的是本次公开页面，不保证所有网站均可抓取 |
| DigitalBuddy 官方网页镜像 | 重试成功：16 个正文块、6,061 字归档正文；17 张归档图片均可回读 | 与上述本地 HTML 快照为不同的摄入路径 |
| 独立图片 | 原图归档已通过：当前原件、历史原件和图片引用 SHA-256 一致；零正文块 | 图片理解未通过；作为文字来源创建研究稿被 400 拒绝，且没有新增稿件 |

首次请求曾因本机代理 fake-IP 触发 SSRF 检查返回 400；精确域名配置后，上表各正文均实际解析、回读并核对。首次失败和最终报告分别保留（`*-before-image-archive.json` 为首轮副本），没有绕过公众号登录或验证码。可重复脚本：`scripts/research-embodied-acceptance.py`，证据在 `artifacts/research/product-audit/embodied-report.json`。

配套研究稿库：`615ed527-4030-46f0-aa33-868a2ea8d30c`；稿件：`25d33cb0-59e7-4e79-aeda-81d7d930f30c`。稿件明确标注「人工流程验收，非 Kimi 生成」。已经验证：

1. 关联上述两份真实材料，人工初稿形成 v1。
2. 两轮带明确修改意见的人工提案分别接受为 v2、v3；意见、前后正文、提案和接受事件均保留。
3. 过期接受/保存返回 409，正文和事件不被覆盖；v1/v2 历史可读取。
4. 人工写作规则仅保留为候选，未激活为用户实际偏好；自动学习保持关闭。
5. 无模型生成请求真实失败，提示词和失败事件保留，成功保存的正文不变。
6. Markdown 导出回读与最终正文/来源一致。

脚本：`scripts/research-embodied-writing.py`；报告：`artifacts/research/product-audit/embodied-writing-report.json`；导出：`artifacts/research/product-audit/embodied-workflow-draft.md`。这证明持久化、审阅与历史链路，不证明真实模型生成或长期写作收益。

## 本机配置、保全与验证

更新前的一致性备份位于 `.research-backups/product-audit-20261021-before/`。为适配本机代理 fake-IP，保留原有配置项，并仅在 `SSRF_WHITELIST_EXTRA` 登记本轮验证的公开来源：`api.kimi.com`、`www.pi.website`、`mp.weixin.qq.com`、`www.shlab.org.cn`、`img.shlab.org.cn`。未添加通配或保留地址网段。新来源仍需正常安全检查或通过文件快照入库。

Langfuse 运行连通性未验证；没有提供项目地址和凭据，仍关闭。业务写作历史保存在平台自身数据库，与遥测开关独立。候选经验需确认后选用，不能将其描述为已经完成模型训练或已证明持续自我改进。

后端最终归档/研究回归为 7 包、91 个顶层测试通过；Langfuse 29 项、数据库 9 项通过，另 3 个 PostgreSQL 集成测试因没有专用测试数据库跳过。前端研究/导航/归档/上传状态聚焦回归 96 项通过，样式约束 10 项通过，类型检查通过；浏览器发现的弹窗关闭问题修复后，研究模块 52 项再次通过，类型检查再次通过。

完整应用和前端已重新构建并部署，本机五服务保持运行。部署前后检查 23 张表、75 份原件/导出及持久卷挂载，四项保全检查全通过；配置比较以本轮明确添加精确域名之后为基线。之后新增的验收资料、模型配置和合成文稿是明确的测试写入，不属于覆盖原有数据。

部署后无模型业务流程使用全新用例复验，9/9 通过：保存和日志、候选确认、撤销、版本恢复、失败提示词、自动学习开关及私有/共享只读权限。首次复用已共享的旧用例造成一个预期不匹配，已保留记录并修正脚本前置检查，详见兼容性报告。

浏览器已确认：收件箱与我的资料切换后 URL、资料集和内容不同；六份真实资料状态正确；原图可以预览且明确尚未识别；研究稿 v3、v1/v2 历史和引用均可见；模型配置存在且真实连接测试明确显示 401。最终前端追加部署后，版本弹窗的可访问关闭按钮和 Escape 关闭已再次实测。未把静态全量清单描述为全平台逐个按钮的浏览器验收。

证据路径：

- `artifacts/research/product-audit/preservation-before-rollout.json`、`preservation-after-rollout.json`
- `artifacts/research/product-audit/build-final.log`、`frontend-final-build.log`、`frontend-dialog-build.log`、`frontend-dialog-rollout.log`、`rollout.log`
- `artifacts/research/product-audit/writing-memory-fresh/workflow_no_model-ui-report.json`
- `artifacts/research/product-audit/ui-workspace-final.png`、`ui-writing-final.png`、`ui-kimi-connection.png`

![工作台实机截图](../artifacts/research/product-audit/ui-workspace-final.png)
