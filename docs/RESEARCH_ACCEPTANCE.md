# 团队研究工作台验收记录

记录日期：2026-10-20。基于 WeKnora `v0.8.3`，上游提交为 `3e22822c1d2397be730c458c7539d0eb8c113a24`。

**当前结论：本机五服务环境中的收集、共享、研究稿审阅和导出流程已完成下述功能验证；完整生产上线门槛尚未通过。** 50 份批量验收资料是合成的人工草稿。本轮没有配置真实聊天或 Embedding 模型，外部内容网络连通性也未完成验证，不能据此宣称真实公众号/论文收集、AI 引用准确性或向量混合检索性能已达标。

本次运行的五个镜像 ID、上游版本与依赖锁文件 SHA-256 记录在 [research-release.lock.json](research-release.lock.json)。这些 ID 标识本次已测构建；迁移服务器时应保存/载入对应镜像，或发布到自己的镜像仓库后固定摘要。

本记录中的“通过”仅指对应测试范围。历史失败报告保留，不以最终局部通过覆盖失败历史。报告和样本位于本地 `artifacts/research/`，默认忽略、不提交版本库；账号、令牌及测试状态保存在独立的隐藏文件中，不应加入交付报告。

## 交付范围

| 环节 | 已实现的产品行为 | 验收边界 |
| --- | --- | --- |
| 收集 | 私密收件箱；原生 URL、文件、笔记入口；书签 HTML 预览和批量导入；原生队列中的状态与重试 | 浏览器扩展复用上游入口，未代用户安装；需要登录、验证码的内容不保证自动获取 |
| 来源与版本 | 同一知识 ID；归档网页正文、快照和图片；文件替换前保全旧原件，原件版本独立留存；并发重复导入合并 | 已验证本地合成文件；远程论文下载分支有单元测试，尚未以真实外部论文完成端到端验收 |
| 团队协作 | 使用原生 Workspace、组织与权限；勾选资料调用原生移动；核对目标实际受众；保留标签和来源 | 原生共享主体是 Workspace/租户；私密收件箱要求个人空间只有一名活跃成员 |
| 研究稿 | 独立用途资料集；四种模板；原生 Wiki 页面及版本；修改提案、差异确认、版本冲突和回滚 | 人工修改闭环已跑通；真实模型生成质量未验收 |
| 导出 | Markdown、Word、PDF；原件/正文/图片/来源版本/manifest 的项目 ZIP；任务状态、重试与下载权限 | 固定研究模板，非通用排版引擎；Word 复杂版式往返不保证保真 |
| 图谱与记忆 | 提供原生 Wiki 图谱和待确认偏好入口 | 无模型情况下，未验证主题自动生成、图谱质量或长期偏好学习 |

运行环境只使用 frontend、Go app、docreader、PostgreSQL/ParadeDB、Redis 五个核心服务。Word/PDF 转换复用文档镜像已有的 python-docx、LibreOffice 和中文字体，不增加导出服务、Pandoc、第二套向量库或 Agent 框架。macOS 使用浏览器及启动脚本；录音、原生 macOS 客户端、飞书双向同步、主动 Agent 和深度视频检索不在本次范围内。

## 已取得的实测证据

| 验证范围 | 实际结果 | 证据 |
| --- | --- | --- |
| 50 份资料、私密收件箱、组织共享、精确受众、撤权及移除成员 | 五项流程检查通过。书签任务可记录部分失败，成功交给原生摄入不等于远程正文已成功解析 | [capture-report-20261020-185148.json](../artifacts/research/capture-report-20261020-185148.json) |
| 原生解析、跨库移动、标签与正文/分块权限 | 两份合成资料真实经过原生分块、后处理及移动；知识与分块 ID 保持，标签和正文保留，撤权后禁止读取 | [capture-report-20261020-183454.json](../artifacts/research/capture-report-20261020-183454.json) |
| 原件同 ID 替换、历史证据、重复替换与写权限 | 两版原件保留；重复替换不增加版本；共享 Viewer 不能替换原件 | [capture-report-20261020-185205.json](../artifacts/research/capture-report-20261020-185205.json) |
| 并发收集与移走后再次收集 | 四个并发上传、四个移走后并发重导只得到一个知识 ID；分块、版本、内容和观察时间不变 | [capture-report-20261020-190003.json](../artifacts/research/capture-report-20261020-190003.json) |
| 共享期间组织受众并发变化 | PostgreSQL 实测成员插入、角色更新、删除及组织变更在授权事务期间等待；事务释放或回滚后恢复 | [audience-lock-pg-report.json](../artifacts/research/audience-lock-pg-report.json) |
| 研究稿与三格式导出 | 14 项通过：私密隔离、跨来源受众检查、原生 Wiki 绕过阻断、用途标记不可降级、过期写入 `409`、接受/拒绝/回滚、导出与状态恢复等 | [drafts-report.json](../artifacts/research/drafts-report.json) |
| 历史导出的实时权限 | 导出任务完成后撤销来源共享，研究稿读取、接受提案、旧导出状态及下载均拒绝；恢复共享后继续工作 | 同上报告中的 `source_revocation_is_rechecked_before_read_accept_and_historical_export` |
| 版本原件与离线资产包 | 三项通过：所有者读取两版原件，字节及 SHA-256 与上传内容一致；Viewer 可读归档正文但不能下载原件；ZIP manifest 可定位两版历史原件 | [source-assets-report.json](../artifacts/research/source-assets-report.json)、[source-original-versions.zip](../artifacts/research/source-original-versions.zip) |
| Word/PDF 再导入 | 导出的中文标题与表格内容通过原生文档服务重新解析并形成分块，原件版本同时归档 | [document-round-trip-report.json](../artifacts/research/document-round-trip-report.json) |
| 浏览器中的实际操作 | 在生产入口 `8090` 实际保存笔记、创建模板研究稿并从 v1 保存为 v2、下载 PDF | [工作台截图](../artifacts/research/workbench.png) |
| 版式与脱离平台读取 | Word/PDF 使用可用中文字体；渲染检查未见乱码、内容溢出或 JSON 尾注。Word/PDF 列出简短来源版本，Markdown 保留完整机器记录；ZIP 内正文非空、可独立读取 | [Markdown](../artifacts/research/research-draft.md)、[Word](../artifacts/research/research-draft.docx)、[PDF](../artifacts/research/research-draft.pdf)、[项目包](../artifacts/research/project-assets.zip)、[最终渲染页](../artifacts/research/final-docx-preview/page-1.png) |
| 独立备份恢复 | 七项只读核对通过：归档校验和、五服务镜像 ID、50 份资料、两版原件、研究稿版本与修订、三格式历史任务与下载、离线 ZIP 正文 | [backup-restore-report.json](../artifacts/research/backup-restore-report.json) |

这里的原生分块与移动测试通过公开 API 关闭模型相关处理，没有使用假模型、伪造模型结果或直接修改数据库完成状态。它验证了正文、标签和分块的生命周期，**没有覆盖已生成向量、Wiki 图谱与模型引用同时存在时的完整一致性**。

初次书签导入报告 `183213` 与初次并发去重报告 `185209` 曾失败，之后修复并分别复测为上述结果。不得将旧失败文件删除后写成“首次运行全绿”。

## 自动化测试与剩余失败

研究定制的 Go 专项测试覆盖来源受众校验、租户隔离、并发幂等、原件归档失败回滚、版本冲突、自动 Wiki 写入闸门、转换失败重试、图片归属、旧资产包失效及来源资产下载权限。最终文档镜像中的 Python 专项 **21 项通过：网页解析 13 项、固定模板导出 8 项**。导出覆盖中文表格/引用/图片、转换超时后释放、独立目录与 LibreOffice profile、子进程组清理、体积限制及真实 PDF 转换。

本轮完整运行的 Go **业务相关包套件未全绿**：service、repository、handler、router、container、types、docparser 等包中有 **44 个失败测试**，在固定 tag 的未修改原版、相同环境下复现了同一组 44 项，`custom_only_failures` 与 `baseline_only_failures` 都为空。对照报告还记录了 DNS 返回 `198.18.*` 的安全拒绝、既有 formatter 失败和系统 Python 3.9 缺少 `tomllib` 的复现。这个结果只能说明已测包的对照中没有新增失败项，不能将该套件标为通过，也不是全仓 `go test ./...` 已完成的声明。详见 [baseline-test-comparison.json](../artifacts/research/baseline-test-comparison.json)。

前端类型检查、生产构建通过；安全与渲染聚焦测试 31 项通过，工作台最终专项 25 项通过。两组包含重叠，不相加为总测试数。Mermaid 的五条直接安全公告已通过最小版本修补处理，但 `npm audit` **仍报告 14 个受影响包条目：1 critical、8 high、2 moderate、3 low**。剩余依赖的实际运行路径、修复要求和风险接受边界见 [RESEARCH_SECURITY.md](RESEARCH_SECURITY.md)。未进行完整渗透测试，也未完成 Go/Python/LibreOffice/镜像 OS 的全量安全扫描。

## 备份恢复与性能

### 备份恢复

已生成本地一致性备份 `.research-backups/final-release/`，包含数据库、原件文件卷、Redis 持久数据及恢复所需配置，并恢复到独立项目 `context-restore-verify`、本机端口 `8092`。原项目没有被覆盖；验证结束后已停止恢复实例，保留主平台五服务。备份含敏感配置，不能公开上传或加入 Git。

[恢复报告](../artifacts/research/backup-restore-report.json)七项全部通过，包括 50 份资料的 ID/元数据、两版不可变原件的字节、研究稿正文/版本及三条修订、三格式历史任务与文件下载，以及 ZIP 中全部 50 份正文的独立读取。

首次恢复发现新 ParadeDB 已预建 schema 导致 `pg_restore` 重复对象错误。恢复程序已仅对全新目标使用 `--clean --if-exists`，重跑成功；仍拒绝覆盖已有项目与数据卷。核对命令只读：

```sh
python3 scripts/research-restore-acceptance.py \
  --backup .research-backups/final-release \
  --base-url http://localhost:8092 --project context-restore-verify
```

### 性能

早期测量只覆盖“知识列表关键词过滤”和“全局标题检索”，每入口三轮、每轮 300 次有效请求，报告明确排除了模型调用。这些入口不是向量混合检索。

早期 [baseline](../artifacts/research/benchmark-baseline.json) 与 [custom](../artifacts/research/benchmark-custom.json) 报告中，全局标题检索 P95 从约 4.68 ms 变为 6.25 ms，增幅约 **33.6%**。该轮未匹配完整数据库状态，不能作为最终验收比较；原始结果保留作为排查证据。

最终复测在同一硬件、同一恢复实例数据库、同一组 50 个资料 ID 上进行，依次使用官方原版 app 和定制 app。每入口三轮，每轮 30 次预热后记录 500 次请求；下表 P95 为三轮 P95 的中位数。

| 入口 | 原版 P95 | 定制 P95 | 变化 | 本轮判断 |
| --- | --- | --- | --- | --- |
| 知识列表关键词过滤 | 2.854709 ms | 2.551583 ms | -10.62% | 未超过 +10% |
| 全局标题检索 | 5.281958 ms | 4.986708 ms | -5.59% | 未超过 +10% |

证据：[匹配数据原版](../artifacts/research/benchmark-restore-matched-baseline.json)、[匹配数据定制版](../artifacts/research/benchmark-restore-matched-custom.json)。结果只支持这两个元数据/标题接口在本轮没有观察到超出门槛的退化，不能推广为通用加速，也不能替代真实向量混合检索的 P95 验收。

## 重现命令

以下命令用于隔离的本机验收环境。脚本会创建合成资料和共享关系，权限用例会短暂撤销共享或移除测试成员后恢复；**按顺序运行，不要与同一组织的其他权限测试并行**。状态文件使用现有忽略规则保存凭据，不要打印或公开它们。测试通过与否以报告内容及退出码为准。

```sh
python3 scripts/researchctl.py up --build
python3 scripts/research-qa-setup.py
python3 scripts/research-live-acceptance.py --module capture
python3 scripts/research-live-acceptance.py --module native
python3 scripts/research-live-acceptance.py --module originals
python3 scripts/research-live-acceptance.py --module dedup
python3 scripts/research-drafts-acceptance.py --module drafts
python3 scripts/research-drafts-acceptance.py --module source-assets
python3 scripts/research-document-acceptance.py
```

`--module capture` 包含 50 份草稿初始化。`--module native` 验证无需模型的原生分块和移动；`--module move` 要求另外准备真正处理完成的资料，不能用未发布草稿替代。首次本轮旧原件保全验收，还在更新前使用 `--module original-seed` 上传了第一版文件，再在新版执行 `--module originals`。

专项测试可使用项目要求的 Go/Python 环境执行：

```sh
go test ./internal/application/service ./internal/application/repository \
  ./internal/handler ./internal/router ./internal/types ./internal/container ./internal/infrastructure/docparser \
  -run TestResearch -count=1
PYTHONPATH=. python -m pytest docreader/tests/test_web_parser.py docreader/tests/test_research_export.py -q
```

Python 环境没有 LibreOffice 时，真实 PDF 单元测试会跳过，**跳过不能替代实际文档镜像里的转换验收**。上述 Go 命令只跑定制专项；业务相关包基线对照应在原版与定制版相同工具链/网络条件下运行相同包集合、去掉 `-run TestResearch` 筛选并保留全部失败，不能只引用专项测试。

前端验证：

```sh
cd frontend
npm run type-check
npm test -- src/utils/mermaidSecurity.test.ts src/utils/mermaidStreaming.test.ts \
  src/utils/documentPreviewMarkdown.test.ts src/utils/markdownDomPurify.test.mjs \
  src/views/research/workspaceState.test.ts src/views/research/researchCitations.test.ts
npm run build
npm audit --json
```

备份、全新项目恢复以及性能命令见 [RESEARCH.md](../RESEARCH.md)。性能脚本支持 `--base-url` 与 `--series`，可在恢复出的验收实例保存独立的匹配数据测量；只有实际切换了官方原版或定制二进制后，才可分别使用 `--label baseline` 或 `--label custom`，标签本身不会切换版本。

## 尚未完成的上线门槛

| 门槛 | 当前状态与下一步 |
| --- | --- |
| 50 份真实代表性资料 | 待验收。需要公众号、论文、不同文件类型、图片/扫描件等真实样本，逐项对照原件检查收集完整性 |
| 云端聊天与 Embedding 模型 | 未配置。本轮没有实际生成摘要、向量或研究提案；需验证模型连通性、调用失败恢复、成本与速率限制 |
| 引用准确性 | 待验收。代码能拒绝不存在的来源定位、无引用和截断，但不能据此证明模型陈述与引用证据相符，需要人工核对 |
| 移动、更新和撤权后的完整一致性 | 正文/标签/分块/研究稿及导出权限已有实测；有模型的向量、图谱和引用路径还需完整验证 |
| 向量混合检索 P95 增幅 ≤10% | 待验收。需相同硬件、相同真实数据、相同索引及模型条件，排除外部模型耗时后对照原版 |
| 并发导出不影响上传 | 队列隔离、转换互斥、超时与重试有实现及专项测试；代表性负载下的上传成功率、尾延迟和资源峰值尚未完成压力验收 |
| 安全与公网部署 | 剩余依赖风险未清零；未配置香港/海外服务器、域名、HTTPS 和实际外网访问。公网发布前需处理安全复核中的门槛 |

满足这些条件并更新本记录后，才能评估是否达到原计划的上线标准。当前交付可作为本机功能验证与下一轮真实业务验收的基础。
