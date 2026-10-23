# DeepSeek 接入与具身智能写作续验

日期：2026-10-21（Asia/Shanghai）。承接此前产品审计；本轮依据用户明确要求新增 DeepSeek、保留 Kimi，并执行真实模型工作流。材料使用公开 π₀ 论文、DigitalBuddy 官方文章及已归档图片，不代表对最新研究进展的全面覆盖。

## 模型与凭据

- 官方接口：`https://api.deepseek.com`；模型 `deepseek-flash`，以官方文档及该账户真实 `/models` 返回值为准。
- 当前验收账户 `ui-qa@example.invalid` 新增对话「DeepSeek Flash · 研究写作」和视觉「DeepSeek Flash · 图片识别」。原有两个 Kimi 模型配置保留；配置前后回读比较相同。
- 官方模型列表、真实 Chat Completions 请求均 HTTP 200；通过平台已保存配置调用也 `ok=true`，返回 `CONNECTED`。
- 凭据由本机图片识别读取，仅用于本机保存和官方认证；没有放入源码/报告，临时明文文件在保存后删除。报告只记录模型 ID、非敏感能力和调用结果。
- 本机代理把官方 API 域名解析为保留的 fake-IP 地址，因此沿用既有精确域名配置，仅新增 `api.deepseek.com`；没有开放网段或通配域名。

证据：`artifacts/research/deepseek-acceptance/connectivity.json`、`platform-connectivity.json`、`models.json`、`network-config.json`。官方接口说明：[DeepSeek API](https://api-docs.deepseek.com/)。

## 具身智能真实文稿

研究库：`615ed527-4030-46f0-aa33-868a2ea8d30c`；新稿：`19e68f78-5e8d-41c4-9632-1c2b113a0a46`，标题「具身智能调研 · DeepSeek真实模型验收稿」。原人工 v3 验收稿保留。

使用 π₀ PDF 与 DigitalBuddy 官方镜像正文，合计 145 个分块，含引用头的完整输入为 81,809 UTF-8 字节，低于研究模块 120,000 字节预算；没有静默截断。生成结果先作为提案保留，核对重要结论与来源后才接受。真实模型流程 7/7 检查通过：

- 初稿接受为 v2；第一轮改成结论前置、区分来源陈述与待验证判断，接受为 v3。
- 第二轮增加「待验证问题 / 所需证据 / 负责人 / 状态」表格，涵盖泛化、仿真到真实迁移、部署稳定性；负责人均待分配，未编造已完成结论，接受为 v4。
- 三次提案分别引用 21、20、19 个真实来源分块。关键数字曾对照 10 个来源分块检查；这不是对全部研究结论的独立复现。
- 保存 3 份历史版本及每次提示词、完整修改前后正文、提案和来源版本。过期保存返回 409，原人工 v3 稿内容与版本不变。
- 两轮修订的自动学习均完成，生成 6 条待确认规范；含初稿共 9 条候选，未自动确认为长期偏好。新稿自动学习保持开启，使用 DeepSeek。
- Markdown 导出与当前正文、引用一致，7,822 字节，SHA-256 `ce9c933e60f585084d8735eb8f2af33bde2e845d2fcf1a799604465f597bbd45`。

证据：`artifacts/research/deepseek-acceptance/embodied-writing/embodied-ai-writing-deepseek-report.json`；最终稿：`embodied-deepseek-final.md`（同目录）。

## 独立写作学习闭环

真实 DeepSeek 推理完成 7 个唯一检查，均通过。首次旧手工样本的合法空提炼保留；仅将缺失的手工链改为“事实不变、表达变化”的新样本补验，没有把两次执行描述成一次无异常通过。

- 自动学习从明确保存的前后改写生成 3 条风格候选；未确认时禁止选用。
- 确认后，真实模型复用「区分判断、依据边界与行动项」，提案保存所采用规则的版本快照。
- 手工事件提炼 2 条候选；真实反馈回流又提炼 3 条候选；重复提炼幂等。
- 撤销后新调用返回 400，撤销后接受旧待审提案返回 409；历史使用快照和来源链不丢失。
- 独立合成测试最终关闭自动学习，并撤销两条已确认测试规则，避免把测试风格默认为用户长期偏好。

这些结果证明本次学习流程可运行；没有测量长期个人风格提升，也没有训练或微调模型。真实具身智能稿的自动学习状态另行记录。

证据：`artifacts/research/deepseek-acceptance/writing-memory/combined-model-real-report.json`、`automatic-learning-evidence.json`、`automatic-rule-reuse-draft.md`。

## 本轮发现及修复

1. **保留 Kimi 后的默认模型选择**：研究编辑器此前取列表首项，重开可能选中旧模型。改为优先采用当前研究库配置的写作模型，并限定在当前调用者可访问的模型列表内；保留共享库跨租户回退。3 项聚焦测试与类型检查通过。
2. **图片描述进入写作**：图片识别成功后正文接口的默认 text 过滤遗漏 `image_ocr` / `image_caption`。研究模块改为显式读取正文、OCR 和描述三类证据，其他调用仍保留 text 默认查询；未识别原图继续拒绝写作。真实 SQLite 仓储回归覆盖 OCR-only、caption-only、混合引用、来源变化冲突和无关分块排除。研究服务 62 项测试通过。实际单图写作续验结果见下方部署完成记录。
3. **写作经验弹窗关闭**：页面复验发现管理、编辑和来源差异弹窗遗漏可访问关闭按钮，Escape 参数也不正确。统一关闭处理，补充三个具名按钮；Escape 只关闭最内层，提交中禁止关闭，未保存输入需确认放弃。14 项针对性回归和类型检查通过。

## 部署与单图写作续验

- 应用与前端已在本机五服务部署中更新。首次部署前后只读指纹比较：23 张业务表、121 个文件，以及环境配置、持久化挂载均保持一致，四项检查全部通过。
- 独立图片识别 8/8 通过：OCR 86 字、描述 213 字，原件及原件历史 SHA-256 不变。修复部署后复用原验收稿，真实 DeepSeek 生成并引用 OCR、描述两类证据，审阅后接受为 v2。
- 单图写作续验 8/8 通过：实际生成、两类分块引用、全部引用属于所选图片、v2 接受、v1 历史保留、首次失败事件保留、来源版本追溯和 Markdown 导出。没有重新解析图片或修改主研究稿。
- 浏览器验证 v4 正文预览、三份历史版本、默认 DeepSeek 模型、自动学习开启及完成状态、候选经验待确认。研究库共 10 条候选包含旧人工候选 1 条、新稿初稿候选 3 条及两轮修订候选 6 条，已启用为 0。两条结论表达候选有内容重叠，未擅自合并或启用。
- 最后仅更新前端弹窗补丁；23 表、122 文件及配置、挂载保全比较再次全部通过。浏览器确认三个具名关闭按钮、Escape 只关闭最内层、主层 Escape 退出，以及未保存输入取消关闭后保留；临时表单测试文字已清空，没有保存候选或触发模型。

证据：`preservation-before.json`、`preservation-after.json`、`preservation-ui-after.json`、`image-understanding/writing-evidence.json`、`image-understanding/image-evidence-accepted-export.md`、`browser-workflow.json`、`ui-final-draft.png`、`ui-history.png`、`ui-learning-history.png`、`ui-learning-candidates.png`、`ui-deepseek-writing-ready.png`，均在 `artifacts/research/deepseek-acceptance/` 内。

## 保留的边界

- 资料集仍未开启向量/关键词索引；本次验证指定来源写作，不证明整库语义检索质量。DeepSeek 对话模型不能代替 Embedding。
- 公众号正文此前已验证入库；24 个公众号外链图片没有全部离线保存，官方镜像有 17 张已归档图片。
- 原件按内容 hash 去重保存历史；同一图片再次识别的 OCR/描述写入当前分块，不单独生成每次识图输出的不可变历史快照。文稿提案会记录来源版本、hash 与观察到的更新时间。
- Langfuse 尚未连接真实项目。业务提示词、提案、正文版本和写作经验记录在平台自身数据库中，与遥测开关独立。
- 模型生成的技术、性能与成本结论仍需核对源材料；本次有限抽查不构成完整研究或商业尽调。
