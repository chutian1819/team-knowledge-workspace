# 研究闭环的 Langfuse v4 真实只读验收

`scripts/research-langfuse-verify.py` 只请求本机 Langfuse 的 `GET /api/public/v2/observations`。它不会调用模型、登录或改写平台、改变学习开关、创建候选或修改数据库。活动由正常平台工作流先产生，脚本随后验证真实观测记录；离线单元测试不构成服务接通证明。

默认从工作区 `.research-observability.env` 读取两个项目凭据，仅保留在进程内存。文件作为数据解析，不执行 shell，不读取其他目录，也不输出凭据、鉴权头、原始错误响应或完整输入输出。目的地址限定 loopback；拒绝 HTTP 重定向，防止带凭据跳往其他站点。

## 运行方式

在开始实际平台工作流之前记录 ISO8601 时间。完成语义检索建稿、生成、人工保存触发自动学习、明确确认候选、使用已确认版本再次生成并接受后，执行：

```sh
python3 scripts/research-langfuse-verify.py \
  --since '2026-10-22T00:00:00Z' \
  --draft-id '<实际研究稿 ID>' \
  --wait-seconds 180 \
  --output artifacts/research/semantic-langfuse-acceptance/langfuse-verification.json
```

将示例时间换为该次操作之前的真实时间。`--draft-id` 可重复；不传时仅分析含 `research.*` 业务跨度的 trace。先依据稿件或会话找 trace，再保留同 trace 的全部相关观察，因此新建稿时尚未分配 draft ID 的检索子跨度仍可关联。起始时间若晚于根请求开始，可能丢失父节点；应修正时间窗口，不能补造跨度。

脚本按 cursor 读取完全部分页，最多默认 100 页、20,000 条观察；超限或 cursor 循环视为失败，应缩小时间窗口。为等待异步导出，每 5 秒重新读取一次完整窗口，默认最多 120 秒；`--until` 可固定截止时间。仅出现完整且一致的链条才会提前通过；接口暂时 503/429 时会重试。

探测鉴权及 API 形状，无须制造活动：

```sh
python3 scripts/research-langfuse-verify.py --probe --wait-seconds 0 \
  --output artifacts/research/semantic-langfuse-acceptance/langfuse-read-api-probe.json
```

探测模式无论有无数据都不会写 `workflow_verified=true`。本轮初次真实探测为 HTTP 200、`data=[]`、`meta={}`，报告状态 `pending`；这仅说明读取接口可用，尚不能验证观测接收。

## 默认必须通过的检查

| 检查 | 必须存在的真实证据 |
| --- | --- |
| retrieval | `research.retrieval` 与原生 `retrieve` 的父子关系；非空查询、有查询向量、实际非零向量命中；选中文本证据的 ID、数量、查询与快照 hash。 |
| query_embedding | 研究检索跨度内有实际 `embedding.embed`，输入与检索查询一致、模型 ID 一致、有模型名称和用量；只有缓存向量不够。 |
| generation | 已完成模型 generation 是提案跨度的子节点，提示事件、稿件、模型、usage、提案 ID 完整；检索快照 hash 与生成上下文一致。 |
| revision | 保存、接受或恢复的事件 ID、基础版本和结果版本可核对。 |
| learning | 已提交保存→enqueue→后台 worker→事件提炼→实际模型调用具有同一 trace ID 和可追溯父链，事件 ID 一致。 |
| candidates | 自动提炼完成并记录非空候选规则 ID/版本；合法空候选不冒充候选创建。 |
| confirmation | 明确的 active 决定关联同一候选和来源事件，版本从候选版递增，发生在提炼完成之后。 |
| reuse | 确认后的同一规则版本同时出现在实际模型 generation、提案和随后接受的修订中，时间顺序正确。 |

可以用 `--require retrieval,query_embedding,generation` 等子集单独诊断。报告保存 `required_checks`，子集通过不代表完整学习闭环通过，不能用缩小要求掩盖缺失。

报告只输出可核对的观察/trace/业务 ID、父关系、时间、版本、选中 chunk 的 ID/score/hash、模型用量及 I/O 的 SHA-256 和 UTF-8 字节数。原始查询、资料、提示词、模型正文和规则说明仅在内存用于比较。Embedding token 数来自当前平台包装器的估算，报告会明确注明，不能视为提供方账单。

退出码：完整请求检查通过为 `0`；有数据但缺少任何必要环节、格式/鉴权错误或分页不完整为 `1`；仍无相关数据或端点暂时不可用为 `2`（pending）。`--probe` 返回 `api_ready` 只代表读取就绪。缺失检查在报告中逐项标为 `fail`，即使顶层因尚无数据而保持 pending；不会把“没观察到”写成通过。

离线断言回归：

```sh
python3 -m unittest discover -s scripts -p 'test_research_langfuse_verify.py' -v
```

这些用例覆盖完整链、父链缺失、查询/快照不一致、缺少研究检索跨度、未确认/晚确认、空数据、分页循环与凭据地址限制，并检查报告不含合成的正文秘密标记。真实接口契约参见 [Langfuse Observations API](https://langfuse.com/docs/api-and-data-platform/features/observations-api)。

## 批量索引观测补验

2026-10-22 实际读取发现：查询 `embedding.embed` 有记录，但首轮 220 块索引没有 `embedding.batch_embed`。原因是 pool 回调经过并发包装器时替换了最外层模型，绕过 Langfuse/debug 包装器。三个包装器现已统一保留传入模型，只在 nil 时回退自身，实际请求仍经过并发 gate。

`pool_observability_test.go` 通过真实 `NewEmbedder`、批处理池、HTTP 测试提供方与 OTLP 测试接收器验证 debug 开/关、nil 回退、输出顺序、每批恰好一条观察和并发上限 2。完整 embedding 包 27 个顶层测试通过。该本机测试的签名身份校验用例需进程级 `SSRF_WHITELIST=weknora.weixin.qq.com` 避免本机 fakeIP DNS 提前阻断；未更改运行配置。

现有 `POST /knowledge-bases/:id/rebuild-index` 不支持 `knowledge_ids`。同内容更新已就绪块不会重建，且更新接口不支持 OCR/描述块，因此不能以此补验原图。独立补验脚本只创建一个清楚标记的 QA 知识库、两段纯测试说明，经 native manual 入库后重建这个小库，不重新解析原件或重建主库：

```sh
# 无参数只显示计划；必须在包装器修复部署完成后执行。
python3 scripts/research-index-observability-smoke.py --execute
```

脚本从 `.research-state.json` 读取合成 ui 登录信息，仅在内存使用；从 `.research-observability.env` 读取观测鉴权。所有产物位于 `artifacts/research/semantic-langfuse-acceptance/index-observability-smoke/`，不保存 token 或原始模型 I/O。严格要求两个入库 trace 和重建 trace 都含真实 HTTP→异步 worker→batch embedding 父链、正确模型与非零用量，重建 embedding 输入数与实际分块数相等，前后分块 ID/正文/类型/启用状态 hash 相同。fixture 支持续跑，测试小库保留供复核。

离线断言使用 `python3 -m unittest discover -s scripts -p 'test_research_index_observability_smoke.py' -v`；它验证缺少批次、父链或覆盖不足必须失败。只有部署后真实 `report.json` 为 passed 才能确认补丁的服务端行为；即使小库通过，也不能补造此前 220 块索引的历史观测。

本轮实际结果：首轮 `index-observability-smoke/` 在构建高负载同时出现 exporter timeout 和接收端错误，缺少入库观察，退出 1；原报告保留，最终失败写于 `failure-diagnosis.json`。恢复后使用另一个独立小库的 `index-observability-smoke-stable/report.json` 为 passed，3 条 HTTP→worker→batch 链全部完整，重建 2 个分块且 hash 未变。稳定复测没有替代首轮失败，也不代表历史 220 块任务的观测已齐全。Redis 空队列下仍有 blocking 连接闲置 Socket timeout 告警，功能通过不能作为团队规模容量或零遥测丢失的承诺。
