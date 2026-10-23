# 写作经验更新与实际验收记录

日期：2026-10-21。当前实例：`context-workspace`，入口 <http://localhost:8090/platform/research>。

## 更新结果

已构建并更新当前运行平台，PostgreSQL 迁移版本为 **125**。应用、前端、文档解析、PostgreSQL 与 Redis 均保持运行，原端口与持久化卷沿用。写作经验作为研究稿的原生模块接入，没有安装 Skill 或新增独立运行服务。

更新前通过 `scripts/researchctl.py backup` 完成并校验数据库、文件、Redis、环境配置备份，位于 `.research-backups/writing-memory-20261021-before-update/`。更新后、创建验收夹具前进行了以下对比，均通过：

| 对比范围 | 结果 |
| --- | --- |
| 18 张原有业务表 | 原有字段的行数与完整行摘要一致 |
| `.env` | SHA-256 一致 |
| 24 份原件与导出文件 | 文件路径与内容合并摘要一致 |
| 持久化挂载 | 类型、来源、目标与读写属性一致 |

证据：[保全检查](../artifacts/research/writing-memory-deploy/preservation-after-rollout.json)、[镜像与部署记录](../artifacts/research/writing-memory-deploy/release.json)、[备份日志](../artifacts/research/writing-memory-deploy/backup.log)。验收随后增加了明确命名的合成资料集和文稿，不能把验收后的总行数再与更新前直接比较。原有业务文稿未被验收脚本修改。

## 使用方式

1. 在「研究工作台 → 研究稿」新建文稿，选择来源知识库、全部或指定资料、文种及可选写作经验；有模型时可直接「创建并撰写初稿」。
2. 正常提交的 AI 写作要求、生成结果、接受/拒绝及保存的版本差异自动记录。手工修改直接保存即可，不需要额外录入学习样本；未提交的输入和升级前缺失的提示词无法补录。
3. 可开启「自动学习我的写作习惯」，在保存或接受修改后后台提炼候选；也可在「写作记录与学习」按次手动提炼。关闭学习后仍保留写作记录。
4. 审阅候选及来源差异，确认后才成为可选经验。下一份相符文种的文稿可明确勾选，模型提案冻结实际使用的版本。
5. 对实际使用记录反馈有用/无用/误用，后续提炼会参考反馈。撤销经验停止后续使用，历史提案和使用版本保留；文稿版本恢复单独形成新版本。

旧稿的自动学习默认关闭；新建界面提供勾选项，必须有可用模型才会启用。模型或后台任务失败不回滚成功保存的正文。候选不会自动变成生效经验。完整方案、权限与功能边界见 [写作经验模块](RESEARCH_WRITING_MEMORY.md)。

## 已完成的验证

| 层次 | 结果与证据 |
| --- | --- |
| Go 研究回归 | 七包命令成功，66 个顶层测试通过，另 13 个子测试通过；其中三个包没有匹配测试。[日志](../artifacts/research/writing-memory-deploy/go-tests-final.jsonl) |
| 前端回归 | 9 个测试文件共 35 项通过，包含研究相关 33 项与相邻辅助函数 2 项。[日志](../artifacts/research/writing-memory-deploy/frontend-tests-final.log) |
| 构建与类型 | 最终生产镜像构建成功；类型检查成功。[构建](../artifacts/research/writing-memory-deploy/frontend-image-final.log)、[类型](../artifacts/research/writing-memory-deploy/type-check-final.log) |
| 迁移 | SQLite 内存库全部 40 个 up、039 down/up 通过；PostgreSQL 独立 schema 事务内 124/125 up、125 down/up 通过并回滚。[结果](../artifacts/research/writing-memory-deploy/migration-check-final.json) |
| 运行实例 API | owner 合成账号 9 项通过、4 项依赖真实模型待验证，0 项失败；ui 合成账号复验同一无模型流程，9 项通过。[owner](../artifacts/research/writing-memory-deploy/live/all-owner-report.json)、[ui](../artifacts/research/writing-memory-deploy/live-ui/workflow_no_model-ui-report.json) |
| 运行实例界面 | 实际保存合成稿 v6→v7，记录显示真实新增行；新建时选择两份资料与一条人工确认的合成经验，仅创建后仍保留所选经验。[差异截图](../artifacts/research/writing-memory-deploy/live-writing-diff.png)、[选择截图](../artifacts/research/writing-memory-deploy/live-writing-selection.png) |

无模型 API 验收覆盖：来源选择、保存与版本冲突、无变化保存、手工候选确认与不可覆盖、撤销、版本恢复、真实失败提示词、学习开关、缺模型失败不回滚、私有/共享只读权限。反馈在此阶段只验证伪造与越权被拒绝，尚未验证真实模型使用后的反馈往返。

首轮夹具创建因原生关键词索引需要 embedding 模型而失败，保留了 `live/first-run*` 记录。随后仅通过公开 API 调整合成资料集的索引选项并真实重解析，第二轮成功。没有配置假模型、模拟推理响应或直接修改数据库状态；此过程不代表检索质量评测。

## 待模型配置后继续

用户已确认会自行在「模型设置」配置模型后通知。当前真实模型验证尚未完成，待继续：

- 从真实修改提炼候选、确认及重复调用幂等。
- 使用知识库和已确认经验真实生成，核验使用快照、反馈写入及后续提炼回流。
- 后台自动提炼成功，以及关闭开关停止后续学习。
- 撤销已实际使用的经验，检查后续调用受限且历史快照保留。

可在同一组合成夹具上恢复，不需要在聊天中提交密钥：

```sh
python3 scripts/research-writing-acceptance.py \
  --module model_real --actor ui --model-id MODEL_ID \
  --output artifacts/research/writing-memory-deploy/live-ui
```

模型须对该验收账号可用；若配置在其他租户，先确认可用范围，再选择对应账号及其合成夹具。真实模型流转通过也不等于已证明中文风格提升；写作质量、检索质量、并发压力及长期收益需另行评估。本次未启用 Langfuse 或外部同步。
