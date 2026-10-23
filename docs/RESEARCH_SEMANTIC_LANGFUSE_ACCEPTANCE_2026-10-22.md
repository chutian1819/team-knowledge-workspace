# 整库语义检索与 Langfuse 闭环验收 · 2026-10-22

## 结论与范围

具身智能验收资料库已完成整库向量索引，真实 DeepSeek 写作、两轮修订、候选写作经验提炼、确认后复用、历史与导出已通过。Langfuse 本机实例的真实读取接口核对了完整的 8 项业务链路，没有把接口可连接、空数据或离线测试当作闭环成功。

本次使用现有隔离验收账户，不代表用户的长期写作偏好；模型文稿由 Codex 对照检索片段审核，用于功能验收，未经用户人工学术审阅。没有微调 DeepSeek 权重。

- 平台：<http://localhost:8090/platform/research>
- Langfuse：<http://localhost:3000>，项目 `embodied-research`
- 新稿：`具身智能·语义检索闭环验收`，ID `296d9adf-801f-47ef-866b-b643339a9875`，v5。
- 原资料库：`9ca4d280-c5ec-4f77-a139-77172a2b61d9`；研究库：`615ed527-4030-46f0-aa33-868a2ea8d30c`。
- 最终正文：`artifacts/research/semantic-acceptance/writing/semantic/final.md`；含来源快照的导出：同目录 `export-v5.md`。

## 为什么不会与原平台重复建设知识库

| 部分 | 实际职责 | 数据归属 |
| --- | --- | --- |
| 原平台 | 原件、分块、语义索引、稿件版本、修改事件、经验候选与确认状态 | 原 PostgreSQL 与文件卷 |
| 本地 EmbeddingGemma | 把资料和查询转成 768 维向量 | 权重在独立模型卷，知识向量仍写原平台 `embeddings` |
| DeepSeek | 依据冻结证据写作、修订、提炼通用写作风格 | 模型结果与使用版本写回原平台；Kimi 原配置保留 |
| Langfuse | 观测调用、耗时、用量、输入输出与业务关联 | 独立数据库、ClickHouse、MinIO、Redis |

Langfuse 的 `embedding` 是记录模型向量化调用的观测类型，不是一个替代原平台的向量数据库。[官方说明](https://langfuse.com/docs/observability/features/observation-types)。当前平台包装器以 `generation` 类型记录、名称为 `embedding.*`，可按名称筛选；不声称已经使用专用 `embedding` 类型。

写作经验学习由平台执行：接受修订 → 异步比较前后稿与真实指令 → 候选规则 → 明确确认 → 后续生成选用规则版本。Langfuse 记录这条链，不负责业务学习。日志会留存部分提示词、检索文本与输出，因此存在用于观测的文本副本，需要保留期管理。

Langfuse 通过异步 OTLP 接收记录；受限 PostgreSQL 角色无权访问原平台业务表，Redis 使用独立容器和数据卷。两个系统仍共享 PostgreSQL 实例、本机硬件和 Docker 磁盘，不是完全独立的故障域。完整运维边界见 [本机部署说明](RESEARCH_LOCAL_OBSERVABILITY.md)。

## 实际验收证据

| 环节 | 本次结果 |
| --- | --- |
| 整库建索引 | 6/6 资料，220/220 可读分块，768 维；纯向量索引，无关键词回退。首次 CPU 建索引 604.4 秒。 |
| 中英文语义检索 | 6 组问题均有实际向量命中，未用知识 ID 过滤缩小搜索范围。 |
| 格式召回 | PDF 目标首位；官网目标首位；微信公众号存档目标第 2、7 位；图片描述/OCR 第 1、2 位，均在 Top 12 内。 |
| 写作证据 | 新稿冻结 8 个片段、5 份来源、查询、排序、分数、模型与内容 SHA-256；模型没有获得整个库全文。 |
| 真实模型写作 | DeepSeek 初稿 v2、修订 v3/v4、经验复用 v5；4 次生成耗时约 35.7、25.1、38.6、40.8 秒。 |
| 多轮修订 | 第一轮结论先行、短段落、区分来源与判断；第二轮新增证据跟进表与研究边界；引用保持在所给证据内。 |
| 自动经验提炼 | 两轮修订自动产生 6 条候选；核对来源事件、模型、样本 hash 和状态，未用人工伪造候选替代自动提炼。 |
| 确认与复用 | 选用“单段单判断且克制表述”，明确限制本验收稿；候选 v1 → 确认 v2 → 实际生成及接受使用 v2 → 撤销 v3。历史仍保留 v2 快照。 |
| 历史与导出 | 当前 v5，4 条历史版本、13 条写作活动记录；Markdown 导出保留正文和检索来源快照。 |
| Langfuse 实际回读 | 真实 v4 API 返回 105 条观察，其中 57 条、12 个 trace 关联本稿；检索、查询向量化、生成、修订、后台学习、候选、确认、复用全部通过，并额外验证稿件范围。 |

检索质量只在这 6 组问题上检查了相关性，不构成大规模召回率基准。图片走 OCR/描述文字语义检索，不是原始视觉向量检索。微信公众号存档正文可检索，但先前记录的 24 张外链图片仍不等于已全部离线归档。

Langfuse 核验不只检查事件存在，还检查父子跨度、trace ID、稿件/修订/规则 ID、版本与时间顺序、查询向量化模型、检索快照 hash、模型实际 I/O 和 usage。DeepSeek 用量取实际返回值；本地 embedding token 数是平台估算，不能当作提供方账单。规则复用证明了版本注入与实际生成，不是孤立因果质量提升实验。

原始证据索引：

- `artifacts/research/semantic-langfuse-acceptance/index-rebuild.json`
- `artifacts/research/semantic-langfuse-acceptance/index-performance.json`
- `artifacts/research/semantic-acceptance/writing/semantic/report.json`
- `artifacts/research/semantic-acceptance/writing/semantic/retrieval-review.json`
- `artifacts/research/semantic-acceptance/writing/semantic/rounds/*/{proposal.json,proposal.md,events.json,review.json}`
- `artifacts/research/semantic-langfuse-acceptance/langfuse-verification.json`
- [真实 Langfuse 核验方法](RESEARCH_LANGFUSE_VERIFICATION.md)

## 本次修复

1. 补全界面已调用但后端缺失的整库重建索引接口、异步任务、互斥与进度状态；直接索引既有分块，保持 ID 和引用。
2. 研究稿增加整库语义检索建稿与冻结证据，生成/接受时核对来源未变，避免把搜索片段与全文范围混淆。
3. 贯通 HTTP → 后台队列 → 模型的追踪上下文，记录修订事件和规则版本，适配 Langfuse v4 I/O 与 metadata。
4. 检索命中项补来源标题，保留排序和片段类型；不改变检索结果。最终浏览器核对 8 项标题均正确、346px 容器内无横向溢出，v5 预览及 v1–v4 历史正常。截图与几何记录位于 `artifacts/research/semantic-acceptance/ui/`，报告为 `initial-controls-report.json`。
5. 批量索引的并发包装器曾覆盖最外层回调，导致真实索引成功而漏记 `embedding.batch_embed`；现在保留最外层包装器。真实构造器、HTTP 提供方、OTLP、debug 开关及并发上限回归通过。部署后在独立小库 `e4cbbc61-16bf-4999-9760-f60ae428a739` 使用两段纯测试说明复验：两次入库与一次重建的 HTTP → worker → batch Embedding 链条全部真实回读，2/2 分块覆盖且 hash 未变。证据：`artifacts/research/semantic-langfuse-acceptance/index-observability-smoke-stable/report.json`。原 220 块建索引发生在修复前，其批量观测没有被回填或伪造。
6. 明确 Langfuse Web 的容器内监听地址，修复服务可访问但 loopback 就绪探针误报的问题。修复后实际 HTTP 200 且容器 `healthy`；宿主机端口仍仅绑定回环地址。

## 保全、验证与部署边界

独立只读检查 8/8 大项通过：旧 220 个分块的 ID/正文/位置、6 份来源、11 条完整来源版本、旧 3 份稿件、原 4 个模型配置和旧 122 个文件 SHA-256 保持一致。来源检查仅排除本次预期变化的索引派生字段；未重新解析原件。四轮共 12 条 prompt/proposal/accept 事件的私有来源/规则/前后稿/模型快照与数据库逐项一致；当前规则撤销为 v3 后，历史仍保留实际使用的 v2。证据：`artifacts/research/semantic-langfuse-acceptance/preservation-final.json`。

新增研究与追踪测试 114 个顶层用例通过，最终检索追踪补充 28 个顶层用例通过；前端相关 17 个用例及类型检查通过。Embedding 包 27 个顶层用例、3 项并发 race 回归通过；既有签名身份用例需仅在测试进程精确放行 `weknora.weixin.qq.com`，避免本机 fake-IP DNS 抢先触发 SSRF 拒绝；运行平台的地址校验未被关闭或泛化放行。本机初始化脚本 5 项回归通过。

扩大检查中另有一项既有 handler 测试因本机 fake-IP DNS 将 example.com 解析到 198.18 地址、触发 SSRF 拒绝而失败；不把整个仓库测试声称为全绿。模拟 OTLP 拒绝的保存测试通过，但未宣称完成真实服务故障下的所有业务演练。

首次小库观测复验与应用构建重叠，出现应用 OTLP 导出超时，缺失两条入库 HTTP 观察及一条批量 Embedding 观察；业务入库及重建本身成功。原失败报告保留在 `index-observability-smoke/report.json`，诊断为同目录 `failure-diagnosis.json`；后续成功复测是独立的一轮，不能追认第一轮完整。Langfuse worker 还存在闲置阻塞连接的 socket timeout 告警；实际接收队列无积压/失败，近期 ingestion 作业完成，不能把该告警本身当作每次数据丢失的证据。异步遥测不是无损业务审计。正式团队服务需将构建负载移出服务机或限制其资源，并监控导出失败与队列健康。

安装前备份位于 `.research-backups/semantic-langfuse-20261022-before/`。最终业务备份 `.research-backups/semantic-langfuse-20261022-after/` 与独立 Langfuse 备份 `.research-backups/langfuse-20261022-after/` 均已保存并通过校验，目录权限为 0700。观测备份另检查数据库归档目录可读、tar 内容及完整 gzip CRC；两套备份各自一致，不是跨系统原子快照，也未执行新环境灾难恢复演练。汇总为 `artifacts/research/semantic-langfuse-acceptance/backup-summary.json`。

Langfuse 正常备份停止 Web 期间，平台实际读取新稿仍返回 v5，记录为 `business-read-during-observability-backup.json`。这是读业务可用性证据，不能扩展为所有写入/模型故障场景均已演练。

备份后服务均恢复运行，已配置的容器健康探针全部通过，Langfuse 健康接口返回 200/OK；再次实际回读 62 条关联观察，8 个业务检查和稿件范围检查仍全通过。记录为 `runtime-final.json` 与 `langfuse-after-backup-verification.json`。最终 Docker 文件系统约剩余 12 GB，本地 Embedding 临时加速用的 CPU 上限已恢复为 2。

本机已经能做上述功能验收；对外团队服务尚未开通。后续仍需 HTTPS、成员独立权限、日志保留策略、磁盘告警、恢复演练和实际并发容量测试。曾发生 Docker 60 GB 虚拟磁盘满，已仅清理可重建的构建缓存恢复，没有删除数据卷。Langfuse 官方 Docker Compose 部署也不提供高可用和自动备份能力，不能把本次单机验收等同于团队生产容量认证。[官方部署边界](https://langfuse.com/self-hosting/deployment/docker-compose)
