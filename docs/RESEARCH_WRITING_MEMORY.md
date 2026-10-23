# 写作经验模块：代码审查、方案比较与接入

调研日期：2026-10-21。旧工具审查版本：[Langfuse_RSI `866aa974e8516ae1973358d8ae9d15ce5b2b9580`](https://github.com/chutian1819/Langfuse_RSI/tree/866aa974e8516ae1973358d8ae9d15ce5b2b9580)。

## 决策

在研究稿中加入原生「写作经验」模块，继承旧工具的证据、候选、人工确认、明确选用、版本反馈和撤销机制；用平台已有的身份、资料集权限、Wiki 修订和模型接入，替代日常 CLI、手工 JSON 搬运、本地资产 Git 和跨仓库 GitHub PR。

核心功能放模块。当前 `ResearchDraftService.generate()` 直接调用 `ModelService.GetChatModel().Chat()`，没有经过聊天 Agent 的 Skills/Memory 流水线；仅安装 Skill 无法影响研究稿生成。平台 Skills 还依赖沙箱和安装镜像，把纯写作能力绑定到这些依赖没有已证实的收益。后续外部写作入口确有需求时，可增加调用平台 API 的薄 Skill；本次没有创建或安装 Skill。

不引入第二套向量库、Python API 服务、Agent 框架、模型凭据或文稿主存储。Langfuse 保留为可选观察与后续评测工具，正常写作不依赖它在线。当前研究 Compose 明确关闭 Langfuse，本次没有擅自启用遥测或向外部服务同步文稿。

## 旧代码实际实现了什么

已审阅全部 `writing_memory/*.py`、提炼提示词、Stop Hook、五组测试、工程/接入/验收文档及原始需求。旧项目是标准库 Python CLI，运行时没有模型推理客户端：

| 环节 | 真实实现 | 平台处理 |
| --- | --- | --- |
| 修改与版本 | `Store.capture` 保存真实文稿快照、原始指令、稳定事件编号；讨论与文件变化分开；保守关联段落 | 沿用真实修改证据；正式稿与历史继续使用 Wiki/revisions，提案保留要求和前后全文 |
| 提炼 | `prepare_extraction` 导出上下文与提示词，再由当前写作工具生成 JSON；`import_candidates` 校验来源与范围 | 显式调用现有模型提炼，严格校验结构，最多生成三条候选 |
| 发布 | `confirm_from_github` 核验真实合并内容及人工审核；无本地自动批准 | 在平台由有编辑权限的人审阅确认；记录批准/撤销人及时间。当前不要求必须由另一位成员确认 |
| 复用 | 按文种、读者、主题字符串精确匹配，导出 Markdown；`record_load` 依赖使用者声明已交给 AI | 当前资料集内按文种限定，用户明确勾选；服务实际注入并冻结所用规则版本 |
| 反馈 | `feedback` 记录有用/无用/误用，关联加载版本；可投递 Langfuse | 绑定实际提案与所用规则，反馈进入下一轮显式提炼，仍需审核新候选 |
| 撤销 | 新导出排除撤销经验，旧加载保留原版 | 撤销停止后续使用，历史提案保留规则正文与版本；旧待审提案需重新生成 |
| 外部写作 | 显式选定 Codex 会话与文件，Hook 只做本地记录 | 本次聚焦平台研究稿，没有接管外部会话或安装 Hook |

旧工具的可靠性设计值得保留，但有两个实测缺口：

1. **历史聊天没有进入提炼上下文。** `SourceImporter.import_file` 保存了历史用户消息，而 `_capture` 只给任务写通用占位指令。`prepare_extraction` 只收 `task`、候选和发布经验，不读取导入消息。临时 Kimi Markdown 复现结果：导入两条消息，真实用户修改要求未出现在提炼上下文，历史消息事件也不是任务轮次。原先 `import → extract` 无法可靠完成历史聊天风格学习。
2. **反馈没有回流。** 旧 `prepare_extraction` 和 `_matches` 不读取 `usage/feedback`；反馈主要落盘/遥测，不能据此声称已实现自动改进。

旧代码没有自动训练、递归优化模型或真正的 `SKILL.md`。其原方案也将当前 RSI 定位为人工反馈驱动的经验积累与跨任务复用。旧测试验证了本地行为，没有证明真实团队 GitHub、Langfuse 或桌面 Hook 已接通。

## 替代方案比较

以下为官方文档与源码核验，不是本团队的写作效果评测。

| 方案 | 已核实能力 | 对本平台的代价与取舍 |
| --- | --- | --- |
| [Langfuse Prompt](https://langfuse.com/docs/prompt-management/overview)、[Datasets](https://langfuse.com/docs/evaluation/experiments/datasets)、[Experiments](https://langfuse.com/docs/evaluation/experiments/experiments-via-ui) | 提示词版本/标签、数据集版本、同数据集比较提示词与模型 | 适合评测；不能替代写作来源权限、经验审批和文稿版本。保留已有接入，真实项目待配置验收 |
| [Langfuse OTEL Experiments](https://langfuse.com/docs/evaluation/experiments/experiments-via-opentelemetry) | 可把自定义工作流轨迹关联实验 | Go 可延续已有 OTEL 接入；需先核对实际 Langfuse 部署版本。本次只补研究生成/提炼的 tracing 路径，没有实现或声称已跑实验 |
| [DSPy GEPA](https://dspy.ai/current/api/optimizers/GEPA/overview/)、[独立 GEPA](https://gepa-ai.github.io/gepa/guides/quickstart/) | 利用分数与文字反馈提出、评估提示词；独立版本支持外部评估器 | 需要可信评分、独立验收集、反思模型和重复调用预算。后续可作为离线优化任务，当前没有证据值得增加运行时 |
| [ACE 论文](https://arxiv.org/abs/2510.04618)、[官方代码](https://github.com/ace-agent/ace) | 按条目增量生成、反思、整理上下文经验 | 借鉴小条目和增量整理；论文任务不能证明中文团队写作收益，不整体引入多角色运行时 |
| [Mem0 OSS](https://docs.mem0.ai/open-source/overview) | 记忆抽取、向量检索、自定义模型和存储 | 与平台 Memory/检索重叠；来源、审核、版本仍需自己实现，暂不采用 |
| [Letta Memory Blocks](https://docs.letta.com/v1-sdk/memory/memory-blocks) | 持久记忆块、跨 Agent 共享与挂载 | 新增有状态 Agent 运行时；不能省掉业务治理，暂不采用 |

没有找到可以直接替换这套业务流程、同时降低接入成本的现成工具。更合适的替换发生在集成方式：保留领域机制，改用平台原生实现。是否值得引入优化器，留给固定任务集的实际对比决定。

## 当前使用流程

进入「研究工作台 → 研究稿」，可以直接选择来源知识库、资料范围、文种、已确认的写作经验及模型，填写写作要求，点击「创建并撰写初稿」。也可仅创建文稿后手工撰写。未指定经验时，正常按材料和本轮要求写作。

写作过程分成两个独立环节：

- **自动记录**：创建文稿、保存正文、提交 AI 要求、生成提案、接受/拒绝以及恢复版本都会留下真实事件。模型调用失败也保留已提交的提示词和失败结果。正常保存形成的版本差异自动关联；手工编辑没有提交提示词时，不伪造一条提示词。采集从升级后开始，不能倒推出旧稿缺失的指令。
- **可选自动学习**：在右侧开启「自动学习我的写作习惯」后，保存正文或接受修改会在原有后台队列提炼候选，无需每次点击学习。开关按当前用户和文稿保存；旧稿默认关闭。新建时可勾选，模型不可用时先记录，配置模型后再开启。

「写作记录与学习」可以查看原始提示词、前后稿差异、提炼状态，或对某一次真实保存直接点击「提炼这次修改」。不再要求先接受一次 AI 提案才能学习手工修改。失败可重试；同一事件的任务重试不会重复创建候选，关闭自动学习仍继续记录写作过程。

提炼使用本次真实前后稿、对应版本区间内提交的提示词及失败/拒绝记录、已使用的规则和实际反馈。输入超过预算会明确失败，不悄悄省略材料后宣称完整学习。没有可复用经验时允许返回空数组。

模型只产生候选。可以查看来源修改，编辑通用要求后「确认启用」；在下一份同资料集、相符文种的文稿中，明确选择最多五条再生成。接受一份文稿不会自动批准风格经验，已撤销或待确认的经验不能用于生成。无模型时仍可保存版本、手工添加和确认经验。

提案显示本次实际使用的经验及版本；在管理面板为它记录有用/无用/误用及原因，区分本人和其他成员反馈。后续真实改稿提炼时结合反馈提出新候选。不合适的经验可撤销，已启用正文不可直接覆盖，历史使用记录保留。

模型提炼可能产生误判，人工审阅应删除具体公司、人物、数字和事实结论，仅保留表达方式。系统不把经验当作证据，不将风格规则加入事实引用白名单；本轮编辑要求和证据要求优先于风格。

## 实现与接口

- `internal/application/service/research_writing.go`：候选/确认/撤销、来源检查、提炼、反馈与注入规则。
- `research_writing_events.go`、`research_writing_learning.go`、`research_writing_inference.go`：不可变写作记录、按用户/文稿的开关、幂等后台提炼和模型输出校验。
- `internal/application/service/research_draft.go`：生成提案时显式加载，保存规则版本快照，提交前重查；接受仍沿用 Wiki 修订和版本冲突保护。
- `frontend/src/views/research/WritingExperiencePanel.vue`：经验选择、管理、来源差异、提炼、确认和使用反馈。
- PostgreSQL `000124–000125`；SQLite `000037` 补齐此前缺失的研究基础表，`000038–000039` 加入写作经验、写作事件和学习任务。没有改动原件或现有文稿格式。

接口均位于 `/api/v1/knowledgebase/:kb_id/research`，使用既有鉴权、资料集访问检查与 `{data: ...}` 返回格式：

| 方法与路径 | 用途 |
| --- | --- |
| `GET /writing-rules` | 读取当前资料集的候选/启用/撤销记录 |
| `POST /writing-rules` | 人工创建候选 |
| `POST /writing-rules/:rule_id/decide` | 用 `base_version` 确认或撤销 |
| `POST /drafts/:draft_id/writing-rules/extract` | 从指定已接受提案提炼候选 |
| `POST /drafts/:draft_id/proposals` | 增加可选 `writing_rule_ids`；手工传入候选正文不能伪造加载记录 |
| `GET/POST /drafts/:draft_id/proposals/:proposal_id/writing-feedback` | 读取/写入真实使用反馈，按调用者、提案、规则更新 |
| `GET /drafts/:draft_id/writing-events?before_id=...` | 分页读取自动记录的真实提示词、版本和修改结果 |
| `GET /drafts/:draft_id/writing-events/:event_id` | 精确读取候选引用的来源记录 |
| `GET/POST /drafts/:draft_id/writing-learning` | 读取/设置当前用户对此稿的自动提炼开关和模型 |
| `POST /drafts/:draft_id/writing-events/:event_id/extract` | 从真实人工保存/接受/恢复产生的差异提炼，按事件幂等 |

规则与反馈存于原有数据库；规则只有表达要求及来源定位，提案自身保存前后稿。提炼元数据记录模型、提示词版本、完整提炼提示词、输入哈希及本轮反馈快照，避免反馈后来更新导致历史不可复原。正式版本继续由 Wiki/revisions 管理，不引入第二份可编辑文档。

写作事件是不可变的审计证据；提炼状态放在独立任务行，不改写历史事件。保存与版本记录使用同一事务，后台模型或队列不可用不会撤销已经保存的正文。任务恢复后重新读取当前用户和成员角色，并复核资料来源权限及学习开关；不能沿用任务入队时的旧授权。

读取、提炼、选用、接受提案及反馈均检查当前资料集权限和原始来源，包括多轮衍生经验的传递来源。来源撤权后拒绝暴露记录；目前任一经验来源失效会使该资料集经验列表整体失败，不静默展示部分结果。重新授权后可继续管理；这是一项需要后续优化的可用性边界。规则撤销与生成落库/接受之间用数据库锁及版本检查串行化。反馈不能使经验自动生效、撤销或训练模型。

## 验证与交付边界

2026-10-21 已更新本机正在运行的 `context-workspace` 实例，入口仍为 <http://localhost:8090/platform/research>。数据库版本为 125，更新前已备份并验证备份；上线后、添加验收夹具前对比了 18 张原有业务表的完整行指纹、`.env`、24 份原件/导出文件与持久化挂载，全部一致。完整结果见 [部署与验收记录](RESEARCH_WRITING_DEPLOYMENT.md)。

- 旧仓库原测试：79 项通过；另做历史聊天提炼缺口复现。
- Go 研究专项：七个相关包共 66 个顶层测试通过，覆盖规则权限、注入与快照、撤销竞态、反馈回流、传递来源，以及事件事务、幂等任务、租约、当前成员权限与长版本链。Langfuse 包全套测试通过。
- 前端本次最终测试：35 项通过，其中研究相关测试 33 项、相邻描述辅助函数测试 2 项；类型检查和生产构建通过。
- SQLite：实际执行全部 40 个 up 迁移，新增事件迁移 down/up 通过。PostgreSQL：隔离 schema、事务内执行 `000124/000125 up` 与 `000125 down/up`，回滚后再正常部署迁移。
- 实际运行 API 验收：9 项通过，真实模型相关 4 项待配置；另一现有合成账号复验无模型流程，9 项通过。覆盖选库、真实保存差异、候选确认、撤销、版本恢复、失败提示词保留、学习开关与成员权限。无模型部分只验证反馈伪造/越权被拒绝，不能替代真实使用后的反馈往返。
- 实际界面操作：合成稿正常保存后从 v6 升为 v7，记录展示真实新增行；新建时选择两份来源与一条经验，仅创建后仍保留所选经验。[版本差异截图](../artifacts/research/writing-memory-deploy/live-writing-diff.png)、[经验选择截图](../artifacts/research/writing-memory-deploy/live-writing-selection.png) 均来自更新后的运行实例。

当前日志位于 `artifacts/research/writing-memory-deploy/`，先前源码验收日志保留在 `artifacts/research/writing-memory/`。所有验收文稿、经验与账号均明确标为合成夹具，没有修改原有业务文稿。单测模型输出是测试夹具；实际运行验收没有配置假模型、伪造推理或修改数据库状态。PostgreSQL 验证未包含真实并发压力测试，撤销竞态由服务单测与代码复核验证。

可重复运行：

```sh
go test ./internal/application/service ./internal/application/repository ./internal/handler \
  ./internal/router ./internal/types ./internal/container ./internal/infrastructure/docparser \
  -run TestResearch -count=1
go test ./internal/tracing/langfuse -count=1
cd frontend
npm run type-check
npm test -- src/views/research/*.test.ts
npm run build
```

本次没有从旧工具导入个人文稿资产。公开仓库本身不含 `.writing-memory`，本次也没有扫描其他目录或接管会话。旧工具可继续用于外部写作记录；其旧数据批量迁入、段落级批示、读者/主题多维范围、语义召回、冲突规则合并和独立审核人策略暂未实现。

经验列表最多返回 200 条，优先返回已启用经验，提案列表最多 100 条；这两个列表尚无分页。写作事件支持游标分页与来源精确读取；按事件提炼的成功任务有幂等保护，不重复建候选。跨事件的语义去重、归档和旧提案提炼接口的重复去重尚未实现。审批沿用现有编辑权限，有权 API 主体也能调用；系统保证的是提炼不会自动启用规则，不宣称服务端已识别真人或强制另一人审核。

已部署的镜像 ID 与数据保留检查见 `artifacts/research/writing-memory-deploy/release.json`。本次没有启用 Langfuse。关闭自动学习且不选择经验即可继续原研究稿生成流程，真实写作记录仍会保存。数据库 downgrade 会删除新增经验、反馈或事件，回退程序前应备份并保留新增表，不能把 destructive downgrade 当成普通开关。

验收时平台没有可用聊天模型，用户将自行在模型设置配置后通知。因此真实模型提炼、复用及反馈往返、自动学习与实际使用后的撤销仍未完成。配置完成后使用既有合成夹具继续验证，不要求在聊天中提供密钥。中文风格收益还需固定模型、材料、任务与文种，分离提炼样本和独立验收样本，对比无经验/已确认经验两组，记录风格符合度、事实保真、人工改动量、修改轮数、耗时与 token。一次“有用”反馈、单测通过或外部论文分数均不能证明 RSI 已持续改善写作。
