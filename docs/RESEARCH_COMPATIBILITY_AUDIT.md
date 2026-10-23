# 研究写作、经验学习与 Langfuse 兼容性复核

初审日期：2026-10-21；追踪兼容补验：2026-10-22。范围为现有研究稿、写作经验、自动学习任务、来源权限、历史记录、Langfuse 接入及数据库迁移。以下初审结论保留当时真实状态；10 月 22 日新增本机 Langfuse 与 v4 追踪的进展见文末，不能将初审的未启用状态理解为后续部署状态。

## 结论

写作经验模块与现有研究稿、Wiki 版本、知识库权限和模型服务共用同一套存储及授权，可以独立于 Langfuse 工作。真实保存、AI 要求、提案、接受/拒绝及恢复都有写作事件；自动学习从实际修改提炼候选，确认后才可在后续写作中选用。它是有审阅环节的经验积累，不能称为模型训练或已经证明写作质量持续提升。

10 月 21 日初审时，本机 `context-workspace-app` 的 `LANGFUSE_ENABLED` 经只读检查为 `false`。该轮将 `compose.research.yaml` 中固定的 `false` 改为 `${LANGFUSE_ENABLED:-false}`：默认仍关闭，允许用户在 `.env` 中显式开启已有远程 Langfuse 项目。因此初审完成时为**原生写作经验功能已接入，Langfuse 服务遥测未启用**。当时没有向真实 Langfuse 项目发送或验证任何轨迹，Go 测试通过或 Compose 可渲染不代表服务连通。后续独立观测部署与真实记录读回应按文末补验结果判断。

## 本轮修复

| 问题 | 修复与保障 |
| --- | --- |
| 成功提炼后的幂等重试只检查保存事件，未重新检查原提炼包含的失败/拒绝提示上下文，可能在上下文来源撤权后返回旧候选 | 返回已有候选前复核每条经验的完整来源链；撤权时返回错误且不返回候选正文。补充同一接口重试的回归断言。 |
| 关闭再开启自动学习，或变更所选模型后，旧排队任务可能继续按旧设置调用模型 | 自动任务同时核对当前设置更新时间与任务创建时间；设置变更使旧任务失效，可对该修改手动重试。重新开启后产生的新修改仍正常自动学习。复核发生在推理前及结果落库前。 |
| SQLite 研究迁移已推进到 39，既有迁移测试仍要求版本 36，导致完整数据库测试失败 | 将期望版本更新到 39，并纳入研究基础/经验/事件共 10 张表与来源、调用身份及租约关键列的检查。覆盖空库、v4 升级保留旧数据和 v16 升级路径。 |
| 研究 Compose 固定关闭 Langfuse，会覆盖用户在 `.env` 中新增的开启设置 | 保持安全默认关闭，并允许显式 `LANGFUSE_ENABLED=true`。继续继承上游已有地址/凭据/调优环境变量，不新增第六个服务。 |

业务修复仅在 `research_writing_learning.go`；测试位于 `research_writing_events_test.go` 与 `migration_sqlite_versioned_schema_test.go`。没有覆盖原有文稿、修改凭据、迁移或重启运行服务；运行服务是否包含本轮修复以随后构建部署记录为准。

## 核对的集成边界

- **资料与模型身份**：研究资料可来自有权读取的来源库；模型配置按调用者租户读取，不借用共享研究库所有者的凭据。异步学习恢复真实用户、当前成员角色及资料权限，不恢复 API key 作为无人值守授权。
- **版本与事务**：正文、Wiki 修订与真实写作事件在同一事务中保存。模型/队列失败保留成功保存的正文；失败提示词有可追溯记录，供应商错误不会原样写入历史而泄露凭据。
- **经验使用与撤销**：候选必须确认，最多显式选用五条；提案保存实际使用的版本快照。生成落库和接受前检查规则版本，撤销停止后续使用，历史快照仍保留。事实引用与风格规则分开处理。
- **来源撤权**：读取、提炼、使用、反馈均复核原始来源和多轮衍生来源；此次补齐成功提炼重试路径。长文稿版本链采用迭代检查，保留循环与节点预算保护。
- **后台兼容性**：标准 Asynq 与 Lite 同步任务执行器均注册 `research:writing-learning`；标准任务进入 maintenance/`low` 队列。事件提炼按事件幂等，使用租约防止过期任务覆盖后续任务。
- **模型输出契约**：研究稿要求完整 Markdown 和现有知识分块引用；提炼要求严格 JSON、最多三条候选。截断响应、非法引用、超预算输入明确失败，不伪造成功。当前完整输入预算为 120,000 字节，长材料需缩小选材范围。
- **Kimi 接入边界**：研究稿生成及提炼均使用统一 `GetChatModel`，传入空 `ChatOptions`，没有在研究功能里指定温度或主动关闭思考。适配器参数仍取决于模型协议及 capability 配置；真实模型 ID、响应模型及 API 权限需在实际验收中核验。本文不以测试夹具证明 Kimi 已连通。

## 当前验证结果

| 验证 | 结果 |
| --- | --- |
| 研究专项七包 | 67 个顶层测试通过（service 54、repository 11、container 1、types 1）；handler/router/docparser 本次筛选没有匹配测试，相关包仍完成构建。 |
| Langfuse 完整包 | 29 个顶层测试通过，包含研究生成/提炼路由选择、关闭状态、任务追踪与 OTEL 导出测试。 |
| database 完整包 | 9 个顶层测试通过；3 个 PostgreSQL 集成测试因未配置专用测试数据库而跳过，不能宣称本轮已执行 PostgreSQL 完整集成测试。首次运行发现的 3 个 SQLite 版本断言失败已修复，最终复跑通过。 |

日志：`artifacts/research/product-audit/backend-research-tests.jsonl`、`artifacts/research/product-audit/tracing-database-tests.jsonl`。测试使用隔离测试数据及模型夹具；真实 API 模型、跨格式入库、多轮写作与自动学习由本次整体流程验收单独记录。

可重复命令：

```sh
go test ./internal/application/service ./internal/application/repository ./internal/handler \
  ./internal/router ./internal/types ./internal/container ./internal/infrastructure/docparser \
  -run TestResearch -count=1
go test ./internal/tracing/langfuse ./internal/database -count=1
```

本机 Go 工具路径为 `/tmp/knowledge-platform-tools/go/bin/go`。

## 可选接入已有远程 Langfuse

只有需要模型调用遥测时才使用此配置；正常资料入库、研究写作、版本历史及经验学习均不依赖它。研究版继续保持五个服务，连接已有云端或自托管的 Langfuse v3+ 项目：

```dotenv
LANGFUSE_ENABLED=true
LANGFUSE_HOST=https://your-langfuse.example
LANGFUSE_PUBLIC_KEY=<该项目的 public key>
LANGFUSE_SECRET_KEY=<该项目的 secret key>
LANGFUSE_ENVIRONMENT=research
```

上述内容只是配置示例，没有写入本机 `.env`。地址应为项目服务根地址，应用通过 `/api/public/otel/v1/traces` 导出；公钥与密钥应属于同一项目。`LANGFUSE_RELEASE`、`LANGFUSE_FLUSH_AT`、`LANGFUSE_FLUSH_INTERVAL`、`LANGFUSE_QUEUE_SIZE`、`LANGFUSE_REQUEST_TIMEOUT`、`LANGFUSE_SAMPLE_RATE` 和 `LANGFUSE_DEBUG` 继续由上游 Compose 透传，可按需配置。

用户明确配置后，需要重建 app 容器以读取环境变量（例如 `docker compose -f compose.research.yaml up -d --no-deps app`）；不要使用仅重启旧容器的命令来期待环境变量生效。使用自定义 `RESEARCH_ENV_FILE` 时，应同时通过 `--env-file` 提供同一路径；`scripts/researchctl.py` 已执行这一处理。只填入凭据但没有显式开启时仍保持关闭。

开启后模型调用的输入、输出及相关元数据可能进入该 Langfuse 项目。业务接口成功不表示遥测导出成功，需在对应项目确认实际生成轨迹。需要关闭时设置 `LANGFUSE_ENABLED=false` 并重新创建 app 容器。此次只验证 Compose 的默认关闭、显式开启和显式关闭渲染；没有修改现有 `.env`、填写凭据、重启容器或实际启用遥测。

## 保留的可用性边界

1. 经验列表最多 200 条，提案最多 100 条，暂无分页；写作事件已有游标分页及精确来源读取。
2. 任一经验的来源失效时，资料集经验列表整体拒绝返回，权限安全但影响其余经验的管理；当前没有安全的「不可访问条目占位」响应协议，未为改善显示而绕过来源检查。
3. 不同事件之间没有语义去重，旧的「从已接受提案提炼」接口也没有按事件的幂等保护。反复使用该旧入口可能产生重复候选。
4. 初审时后台学习仅携带事件 ID，未传递前端请求的 traceparent；10 月 22 日已补上保存到任务的传播与 Lite 恢复路径，详见文末。标准 Asynq 与 Lite 均继续由平台负责学习业务，Langfuse 不参与授权或事务提交。
5. 启用自动学习不会自动批准候选，也不会默认将已确认经验加载到所有文稿。该边界让用户可以审阅和撤销经验；如需更强的默认复用，应另行明确文种/作者范围与冲突规则。

## 真实来源验收发现与原图归档

真实入库测试中的三个公开 URL 最初被拒绝，是因为本机代理 DNS 将公开域名解析为 `198.18.*` 保留网段，触发 SSRF 保护；`CreateKnowledgeFromURL` 没有因此要求额外的摘要或 embedding 模型。本轮保留 URL 与重定向的安全校验，由主任务仅将此次核验的精确公开域名加入本机 `SSRF_WHITELIST_EXTRA`，没有允许整个保留网段。具体运行验证见整体具身智能验收报告，不以代码审查替代网站实际抓取结果。

独立图片原先在上传校验与后台解析两处强制要求 VLM，虽然底层已具备保留原件的能力。新增**显式**原图归档选项，正常识别入口仍保留模型校验，不默认降级：

- 上传 `multipart` 的 `process_config` 传 `{"image_archive_only":true}`，只适用于图片；混合批次仅对图片附加该参数。
- 原件及含图片引用的不可变来源版本保存成功后，`parse_status=archived`、`enable_status=disabled`，说明尚未进行文字识别/图片理解。不调用解析器、OCR、VLM、embedding 或摘要，不创建正文分块。
- 元数据保留 `metadata.process_overrides.image_archive_only=true`。写稿显式选择该图片会被拒绝；「全部资料」遇到原图归档也明确要求先识别或选择可读来源，不静默宣称完整阅读。只有 Markdown 图片包装而没有可用文字/描述的旧图片来源，同样不会被送入写作模型作为文字证据。
- 配置视觉模型后，可显式重新识别：`POST /api/v1/knowledge/:id/reparse`，JSON 为 `{"process_config":{"image_archive_only":false,"enable_multimodel":true}}`。该调用会覆盖持久设置；普通无参数重试仍沿用原图归档意图，不擅自发起识别。

新增服务回归覆盖显式开关与图片限定、无模型/解析器/分块服务的完整后台归档、原件与图片引用保存、存储失败保留原件并清理半成品、任务幂等、选材拒绝以及归档后显式恢复识别。最终 7 包通过、91 个顶层测试通过、0 失败，日志逐行 JSON 解析通过：`artifacts/research/product-audit/backend-image-archive-verified-tests.jsonl`；较早同名 `*-final-tests.jsonl` 曾被并发试跑覆盖，不作为最终统计依据。

## 部署后无模型写作复验

首次复验沿用此前已经共享的合成资料集，8 项通过、1 项在「未共享时同事应不可读取」断言失败：旧用例在上次完成权限测试时已将资料集共享给该同事，实际 HTTP 200 符合既有授权，并非本轮越权。保留旧数据与权限，改用全新合成用例复跑，**9 项全部通过**；报告为 `artifacts/research/product-audit/writing-memory-fresh/workflow_no_model-ui-report.json`。

验收脚本现会在无模型全流程复用已有共享记录时提前提示建立新用例，并跳过会修改学习开关的清理动作；真实模型续验仍可显式使用同一用例的历史样本。新用例定位文件为 `artifacts/research/product-audit/writing-memory-fresh/fixtures-ui.json`。此轮未配置或修改模型，仅验证真实保存、失败提示词保留、候选审批/撤销、历史恢复及私有→共享只读权限流程。

## Kimi Code K3 请求兼容复核

随后主任务在 ui 验收账户配置了两个 `k3` 档案，分别用于写作与视觉；端点为 `https://api.kimi.com/coding/v1`，协议为 `generic / openai-completions`。研究生成与写作经验提炼都传入空 `ChatOptions`，`GetChatModel` 及包装器不注入会话的默认关闭设置。`thinking_always_send=false` 时，请求构造器直接省略推理字段，不会将空选项转成 `reasoning_effort:none/off` 或 `thinking:disabled`。

只投影非敏感字段后发现初始 spec 缺少 `supports_reasoning_effort:true`，因此原先即使选择 high/max，也不会发送强度字段；仅列出 low/high/max 并不意味着排除其他级别。经主任务授权，已通过本机 `PUT /models/:id` 对两个档案做以下最小补齐：

- `compat.supports_reasoning_effort=true`；保留 `thinking_format=openai`、`thinking_always_send=false`、`supports_temperature=false`、`max_tokens_field=max_tokens`。
- `thinking_levels.off/minimal/medium/xhigh=null`，保留 low/high/max 映射。实际能力列表现为 auto、low、high、max。

请求使用 GET 返回的非敏感参数，省略 `api_key/app_secret`；更新接口强制保留已存凭据。名称、类型、来源、描述和其余参数已逐项比较保留。没有读取密钥，也没有发起新的远程模型请求。

更新后针对两档案的 10 个本地请求构造场景全部通过：空研究选项不发送任何关闭推理字段；high/max 分别发送正确 `reasoning_effort`；旧 medium 请求向上归一为 high；off 被标记为不支持，不会向提供方发送关闭字段；采样参数省略、输出限额使用 `max_tokens`、模型名始终为 `k3`。证据：`artifacts/research/product-audit/kimi-offline-wire-verified.json`，非敏感配置为 `kimi-safe-config-verified.json`，能力元数据更新于 `kimi-models.json`。

这些结果只证明本平台组装的请求参数正确；主任务最近一次真实连接仍返回 HTTP 401，尚不能证明凭据有效、提供方实际路由为 K3，或模型写作与视觉理解已通过。参数修正不被记作连通性修复。

## 后续 DeepSeek 真实写作学习验收

用户随后提供 DeepSeek 凭据，由主任务完成官方端点与平台内真实连接验证。本模块显式使用 ui 账户的 `deepseek-flash` / `deepseek` 档案 `a0b0af33-2797-4426-a874-5cac8b351f0d`，没有使用或修改保留的 Kimi 档案，也没有改动另一路具身智能研究稿。模型读取既有合成资料集中的两份已处理来源，所有验收内容均明确标记合成。

两个实际 API 运行合计覆盖 **7 个唯一检查，全部通过**。第一轮的自动学习闭环通过，但较早的人工样本同时调整了事实判断，DeepSeek 返回合法空候选，三项依赖检查据实记为 blocked。保留该结果，脚本改为实际保存「事实相同、只调整表达和结构」的独立前后样本，仅补跑这三项，全部通过；没有将首次运行改写为一次性全通过。

| 真实检查 | 结果 |
| --- | --- |
| 手工事件提炼与幂等 | 生成 2 条候选；同事件重试返回相同 ID；确认「核心判断前置」后才允许用于生成。 |
| 来源写作与反馈回流 | 模型提案保留实际规则版本和有效 chunk 引用；接受后记录反馈，四次更新仍为本人同一反馈行；后续提炼产生 3 条候选并保存反馈来源。 |
| 后台自动学习 | 人工保存真实前后改稿后完成后台提炼，生成 3 条候选，均未自动启用；手动重试幂等。 |
| 自动候选完整闭环 | 确认「区分判断、依据边界与行动项」后真实生成并接受改稿；撤销后再次使用返回 400；已接受提案仍保留所用规则正文与版本。 |
| 撤销与历史保护 | 另一个已生成但待接受的提案，在规则撤销后接受返回 409；文稿版本不变，历史接受记录保留。 |
| 最终开关状态 | 自动学习已关闭；关闭后保存未排队；两条用于复用测试的已确认经验最终均撤销，其余 6 条仍为候选。 |

自动候选提出的规则为：「将核心判断、支撑证据及其适用边界、后续行动分块呈现；证据不足或适用范围有限时单独说明，行动项用简短祈使句列出，不与背景论述混写。」复用后的实际样稿包含「核心判断」「依据与边界」「下一步」，保留“材料不能支持商业成效结论”的限制和来源引用。这是单一合成样本的有限质量观察，不是长期个人风格改善、因果效果对照或模型训练证明。

证据位于 `artifacts/research/deepseek-acceptance/writing-memory/`：`combined-model-real-report.json` 明确列出两轮报告来源；`automatic-learning-evidence.json` 保存真实前后稿、自动规则、提案及所用版本；`automatic-rule-reuse-draft.md` 为实际模型输出；`final-state.json` 为验收后的开关和规则状态。补验原报告在相邻 `writing-memory-manual-completion/`。完整测试仍可通过以下命令执行；多模型并存时必须显式指定已验证模型，脚本不会默认选择保留的 Kimi。

```sh
python3 scripts/research-writing-acceptance.py --module model_real --actor ui \
  --model-id a0b0af33-2797-4426-a874-5cac8b351f0d \
  --fixtures artifacts/research/product-audit/writing-memory-fresh/fixtures-ui.json \
  --output artifacts/research/deepseek-acceptance/writing-memory-rerun
```

该命令会在合成稿中新建真实人工改写事件，并调用指定模型。Langfuse 仍为独立的可选遥测；此次业务学习验收不代表远程 Langfuse 已接通。

## 识别图片作为写作证据的修复

后续真实图片识别已得到 `image_ocr` 与 `image_caption` 分块，但研究生成仍使用仓储的 text-only 默认查询，因此错误地报告“没有识别文字或描述”。本轮只将研究生成调用改为显式读取 `text`、`image_ocr`、`image_caption`；全局默认查询保持不变。模型可用引用白名单由同一批可读分块构建，未纳入向量、摘要、其他来源或其他租户的分块。仅归档图片和只有 Markdown 原图引用的来源继续拒绝写作。

来源版本仍按知识 ID 记录原文件哈希、更新时间及不可变 capture ID/hash，不按分块类型过滤；OCR 与描述共用图片来源的版本。使用真实 SQLite chunk repository 的服务回归验证：OCR 单独存在、描述单独存在、两类并存时均可引用并接受；生成中和接受前 capture 改变均触发冲突，不改写文稿。针对性 5 个顶层测试通过；随后服务包全部 `TestResearch` 共 62 个顶层测试通过、0 失败。日志分别为 `artifacts/research/deepseek-acceptance/image-evidence-tests.jsonl` 与 `image-evidence-research-regression.jsonl`。这是源码回归结果，部署后的真实图片单源写作由主任务另行记录。

## 2026-10-22：研究闭环追踪与 Langfuse v4 补验

新增 `research_tracing.go` 为研究业务操作添加旁路观察：创建、生成/修订、接受/拒绝、人工保存、恢复历史、规则提炼/确认/撤销/反馈、自动学习设置/排队/执行。关联字段只含知识库、稿件、提案、提示及保存事件 ID、正文版本、模型 ID、规则 ID/版本、来源快照 SHA-256 与状态，不将规则正文、反馈说明或凭据复制到这些元数据。已有模型包装器仍按原设计记录模型输入/输出；研究上下文的供应商错误会转为固定安全说明，避免把含凭据的 URL 或响应错误正文发往观测端。

保存产生的任务使用兼容旧格式的 `TracingContext`，由现有 `InjectTracing`/Asynq 中间件续接父 span，稿件会话沿用 `research:<kb_id>:<draft_id>`；Lite 直接处理任务时也恢复 traceparent。后台授权、同事件幂等、租约、禁用学习与规则确认边界继续由平台业务实现，未引入 Langfuse 作为业务状态来源或向量/知识存储。

v4 的兼容修正保留原字段，同时将根 span 表达为受支持的 `span` 类型，并提供根 observation input/output、逐键可筛选 metadata 和所有子观察的 user/session/trace identity。所有 span 仍结束后只导出一次；异步、有界 OTLP 队列不等待外部服务成功才提交业务。实现参照 [Langfuse 自定义 OTEL v4 迁移文档](https://langfuse.com/integrations/native/opentelemetry/migration-to-v4)。

最终源码回归 **2 个包通过、114 个顶层测试通过、0 失败**，逐行 JSON 记录位于 `artifacts/research/semantic-langfuse-acceptance/backend-tracing-research-tests.jsonl`，包括同轮检索桥接回归。新增业务回归使用真实 SQLite 事务与本地 OTLP 测试接收器，覆盖保存→后台提炼→确认规则→复用→接受→反馈→恢复的 ID/版本关联及同一 trace；另一用例让接收器实际返回 401，确认稿件和版本照常提交。这些测试证明客户端行为，**不作为真实 Langfuse 服务验收**。

主任务另行部署本机 Langfuse 4.67.0 独立项目 `embodied-research`，健康与项目鉴权检查由部署记录负责。真实接收验收必须在应用启用并运行真实研究操作后，读取 `/api/public/v2/observations` 核对 `traceId`、`parentObservationId`、研究关联 metadata、模型与 token usage；使用时间范围限制及 cursor 分页。旧 `/api/public/traces` 在该实例不可用，不应据此判断追踪失败。[官方 observations API](https://langfuse.com/docs/api-and-data-platform/features/observations-api) 说明了 v4 的读取契约。本文此处仅记录代码冻结时的验证，最终真实服务结果由本轮部署验收产物补齐。

### 检索快照与只读验证的增量补齐

随后补入 `research.retrieval` 业务跨度，包住真实 `HybridSearch`，查询 embedding 与原生 `retrieve` 均继承其上下文。元数据记录来源库、embedding 模型、实际选中可读 chunk 的 ID/排序/score/content hash、查询 hash/长度和完整冻结快照 hash，不记录原始向量或把正文复制到 metadata；`MatchType` 保持 API 的整数值。新建完成后的 create 事件、后续生成和接受的事件携带同一冻结快照摘要，可以从初次查询关联到真实稿件 ID。索引重建 POST 与独立 hybrid-search POST 也进入 HTTP 追踪，GET 轮询仍排除；索引任务复用既有跨进程载体。

补齐后的针对性回归为 **2 包、28 个顶层测试通过、0 失败**，包含研究检索/追踪、索引任务追踪及 Langfuse 队列传播，记录位于 `artifacts/research/semantic-langfuse-acceptance/backend-retrieval-tracing-final-tests.jsonl`。它是前述 114 项回归之后的增量验证，不能将二者直接相加视为不重复用例数。

真实只读验收脚本为 `scripts/research-langfuse-verify.py`，用法及八项严格检查见 `docs/RESEARCH_LANGFUSE_VERIFICATION.md`。8 个离线解析断言测试通过；真实读取探测曾返回 HTTP 200、空 data，随后部署重启期间暂时不可达，均据实保持 pending。只有后续真实平台活动产生并被 v4 API 读回，完整链条和选中快照关联全部通过，才记录服务追踪验收成功。

### 真实闭环读回与批量索引盲点

2026-10-22 02:41:14 UTC，主任务真实验收稿 `296d9adf-801f-47ef-866b-b643339a9875` 的完整 Langfuse v4 读回通过：8 项业务检查及稿件范围检查全部 pass，105 条读取记录中关联 57 条。`artifacts/research/semantic-langfuse-acceptance/langfuse-verification.json` 保存真实父子关系、四次生成、修订、后台学习、候选、确认和复用的关联与 I/O hash。这证明本次真实闭环可以观察和追溯，不代表自动学习已带来长期质量改善。

同次验收发现独立的覆盖缺口：首次整库索引虽然完成，但没有批量 embedding 跨度。代码定位为并发包装器和 debug 包装器重写 `BatchEmbedWithPool` 的 `model` 参数，导致池回调绕过外层 Langfuse。最小修复只让三个包装器保留传入模型，nil 才回退自身，不改变索引、数据或并发限制。实际 `NewEmbedder` 组合回归覆盖 debug 开关、每批准确一条观察、相同 trace/worker 父节点及最大并发 2；embedding 全包 **27 个顶层测试通过**，3 项针对并发/组合的 race 测试也通过。完整日志 `backend-pool-observability-verified-tests.jsonl` 与汇总 `backend-pool-observability-summary.json` 位于同目录。

部署后用 `scripts/research-index-observability-smoke.py --execute` 在独立小库验证两段纯测试说明的 native 入库与索引重建，不重建原有 220 块或重新解析图片。首轮小库 `66dc3b3e-6262-4936-8415-5d9ab46592ef` 的重建已完成且两条 batch 跨度完整，但两条入库 HTTP 与首条入库 embedding 观察缺失；app 同时记录 processor export timeout，Langfuse 也有 socket timeout 和 request body stream 错误。脚本退出 1，首轮 `index-observability-smoke/report.json` 原样保留（等待结束时的 pending 快照），最终失败及诊断单独记于该目录 `failure-diagnosis.json`，不将缺失改写为成功。

构建负载结束后，先确认 Redis PING 正常、otel/secondary-otel 两队列 wait/active/failed/delayed 均为 0，真实 queue events 有 completed→drained，app/web 近期无新导出错误，再以新小库稳定复测。`e4cbbc61-16bf-4999-9760-f60ae428a739` 的 **两条 native manual 入库链及一条重建链全部通过**：HTTP→worker→真实 `embedding.batch_embed`，模型 ID/用量匹配；重建两次 batch 的总输入数 2 与实际 2 块相同，分块 ID/正文/类型/启用状态 hash 保持一致。重建 trace 为 `072e20fc28944f87a2502d82c3a29c0b`。实际完整报告 `index-observability-smoke-stable/report.json` 保存 3 条链的观察 ID。

稳定复测后 `main-kb-preservation-after-stable-smoke.json` 再次通过：原主库 220 块、6 来源、11 来源历史版本、原有 3 稿、模型配置、索引及 122 个原文件均与保全基线一致。两个小库仅含验收说明，保留供复核。此前 220 块任务的缺失批量观测没有被追溯补造。

功能验收通过不等于团队部署容量验收：空队列下仍观察到 32 个 blocking 连接和 ioredis 闲置 Socket timeout 告警，需继续监测；首轮也证明有界、旁路遥测在导出超时或接收端异常时可能丢失。业务索引和数据可继续完成，但不能承诺观测记录绝不丢失。本轮没有通过任意放大 timeout 隐藏告警。
