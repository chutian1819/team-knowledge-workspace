# 团队研究工作台

在 WeKnora **v0.8.3**（`3e22822c1d2397be730c458c7539d0eb8c113a24`）上补齐个人收集、同源共享、研究稿审阅和开放导出。没有拼接第二套知识库、账号、向量库、模型 SDK 或 Agent 框架；Go、Python 依赖沿用上游，前端仅针对已核实安全公告修补 Mermaid，锁文件一并固定，详见 [安全复核](docs/RESEARCH_SECURITY.md)。

当前部署的功能状态与最少配置缺项见 [2026-10-22 团队试用闭环核验](docs/RESEARCH_TRIAL_CLOSURE_2026-10-22.md)：基础对话、图片理解和新收件箱已通过普通账号公网验证；Rerank、联网搜索及其出站网络仍待补齐。后文早期验收记录保留其当时范围，不代表当前模型仍未配置。

## 本机启动

需要已运行的 Docker（包含 Compose 2.24.4+ 和 Buildx）、Python 3。macOS 可双击根目录的 **启动平台.command**；终端等价操作：

```sh
python3 scripts/researchctl.py up --build
```

打开 **http://localhost:8090/platform/research**。首次编译需要下载固定版本的构建依赖；以后复用 Docker 缓存。启动脚本使用归档构建上下文，兼容中文目录。它只管理 `context-workspace` 项目，不停止其他 Docker 应用。

```sh
python3 scripts/researchctl.py status
python3 scripts/researchctl.py stop
python3 scripts/researchctl.py up
```

首次生成 `.env`，权限为 `0600`；已有配置和密钥不会覆盖。默认只监听本机 `127.0.0.1:8090`，其余四个服务没有宿主机端口。前端是浏览器应用，macOS 无需安装额外客户端。

## 从收集到协作

1. 创建自己的账号，进入「研究工作台」，启用「个人收件箱」。每位成员使用自己的个人 Workspace。个人空间一旦绑定收件箱，就不能再加入其他成员，防止上游同一租户内知识库互通带来意外公开。
2. 当前团队部署由管理员集中管理共享默认模型，同事无需填写密钥。内置助手和新收件箱自动使用已配置的默认模型；收件箱也可继承已有资料集的配置。独立部署仍需管理员先配置真实模型。关闭向量/关键词索引的资料集可先保存笔记、解析文本 PDF 和网页快照，并人工编写研究稿；模型摘要、向量索引、图片理解及 AI 写作需对应的真实模型服务。图片上传可明确选择「仅保存原图」，原件及来源版本会保留，状态为「原图已归档 · 未识别」，不会冒充文字证据；配置视觉模型后可重新识别。
3. 链接、文件、文件夹、笔记先进入收件箱，不要求预先分类。需要浏览时直接剪藏，可在原生「设置 → 发布集成 → Chrome 插件」连接现有官方插件，目标选个人收件箱；[上游连接指引](website-docs/05-clients/06-chrome-extension.md)说明 API Key 配置。插件是可选入口，不增加服务器组件，本次没有代用户安装浏览器扩展。Chrome/Edge 书签导出为 HTML 后，可先预览再批量导入。网页正文仍走原生解析器，并归档解析时实际获得的 HTML、Markdown 和图片。微信公众号受登录、验证码或反爬限制时，会显示失败，可改为上传已保存的网页/PDF 或重试；不承诺绕过访问限制。
4. 在原生「共享空间」建立项目组织，邀请同事的个人 Workspace。研究工作台的「团队项目」聚合该组织的原生共享资料集。
5. 勾选已解析资料，选择项目和目标资料集，核对**实际全部成员**后共享。后台调用原生移动流程，知识 ID 保持不变，逐项显示结果；原生标签会映射保留。目标必须在自己的空间，且受众与选定成员完全一致。未分享目标如果含未选资料或私密 Wiki 页，会拒绝整库授权，避免顺带公开。
6. 分享后该条资料归入项目资料集，仍是一份主记录；同事通过相同 ID 读取更新。个人收件箱会识别已移到自己其他项目中的相同 URL/同类型原件，沿用既有 ID；同内容并发创建用 PostgreSQL 事务锁合并，不重复排解析任务。受限 API Key 没有原资料权限时返回拒绝，不泄露该资料或创建副本。其他原生资料库和带外部来源身份的同步渠道保持上游去重规则。
7. 原件及历史来源版本是证据，摘要、分块和索引是派生数据。文件可从「来源记录 → 更新原件」调用原生同 ID 替换；先保全旧原件再替换，失败不删除旧证据。每个原件字节版本、解析正文和图片归档为不可变文件；网页仍按正文指纹保留版本。重复请求有持久幂等记录，同一版本不重复归档。仅网页装饰或外链图片字节变化、正文未变时，不会单独新增网页正文版本。

原生共享的主体是 **Workspace/租户**，不是单个用户。为了使项目成员和实际阅读者一致，首版要求个人 Workspace 只有一位活跃成员；不要让多个人共用同一账号。原生管理员权限仍适用。

## 研究与文档

- 「所选资料提问」只带所选知识 ID；「项目提问」使用原生知识库问答。两者都沿用原生引用和权限检查。
- 在资料集的「研究稿」页建立专用研究稿资料集。该库的 `wiki_config.research_only` 是不可通过普通设置降级的用途标记，后台 Wiki 整理和原生 Wiki 写入都不能覆盖正式稿。
- 四种模板：项目一页纸、研究简报、公司对比、尽调问题清单。可先人工编辑，也可选文件或整个资料集生成。生成使用现有模型接入；拒绝伪造引用、无引用、截断或超预算的结果。单次选取最多 100 份已解析资料、证据正文预算 120,000 字节；超限明确报错，可缩小范围或用原生检索问答。
- AI 修改先成为提案，显示差异；接受后才保存。人工保存、接受提案和回滚都带基础版本，冲突返回 `409`，界面保留未保存正文。
- 正文与版本复用原生 Wiki 页面及 revision 表。研究稿读者必须也能阅读其所有来源；每次读取、提案、接受及导出都会重新检查。
- Markdown、Word、PDF 导出使用固定研究模板。Word/PDF 复用文档镜像已有的 `python-docx` 和 LibreOffice，使用已有中文字体、独立临时目录和转换进程，设置超时与并发上限。没有新增 Pandoc 或导出服务。
- 项目 ZIP 包含原件、可读正文、图片、来源版本和 `manifest.json`。单包最多 1,000 份资料、200 MiB；超限失败，不会悄悄漏资料。原件 ZIP 沿用原生 Contributor/Editor 权限；Viewer 可阅读共享内容，不因此获得原件批量下载权限。旧 ZIP 中资料被移出、删除或撤权后，下载会关闭。
- 图谱和个人偏好入口复用原生 Wiki 图谱与待确认记忆。首版不增加 Neo4j、Mem0 或独立任务系统。
- 研究稿可组合选择来源知识库与已确认的写作经验再撰写；正常提交的提示词及保存差异自动记录，可按需提炼或开启自动学习。模型只生成候选，人工确认后按文种选用，提案冻结实际经验版本；有用/无用/误用反馈参与后续提炼，撤销保留历史。复用现有模型和权限，无额外服务。方案与边界见 [写作经验模块](docs/RESEARCH_WRITING_MEMORY.md)，当前运行实例的更新与验收结果见 [部署记录](docs/RESEARCH_WRITING_DEPLOYMENT.md)。

## 部署边界

`compose.research.yaml` 仅包含 frontend、app、docreader、PostgreSQL/ParadeDB、Redis。基础运行镜像和构建镜像使用摘要固定；本次实测的五个镜像 ID 及依赖锁文件哈希见 [发布锁定记录](docs/research-release.lock.json)。生产环境应把当前源码和构建出的三个自定义镜像作为同一发布版本；不要让生产机器跟随 `latest`。Redis AOF 使用独立持久卷，任务和进度不会随容器重建丢失。

香港/海外服务器可使用同一份源码和脚本构建。准备好自己的域名、HTTPS 入口、持久磁盘和模型连通性后，调整 `.env` 中的前端地址及监听地址。默认配置仍是本机开发配置；本次没有远程服务器凭据，因此没有声称已经完成公网部署。公网发布前请阅读 [安全复核](docs/RESEARCH_SECURITY.md) 和上游安全配置，关闭或限制公开注册，使用团队私网/受保护入口。

2026-10-22 公网试用补充：平台和数据仍部署在本机 Mac，已切换到 ChmlFrp 中国香港 VIP 节点，固定 HTTPS 入口为 https://team-aiorigin.owo.lat，并验证两账号的真实文件共享、原件替换、版本保全和撤权。公网自由注册已限制；启动、地址查询、独立同事账号开通和运行要求见[ChmlFrp 部署说明](docs/RESEARCH_CHMLFRP_DEPLOYMENT.md)。首轮临时入口已关闭，[早期验收记录](docs/RESEARCH_PUBLIC_TRIAL_2026-10-22.md)保留。这不是 Mac 获得固定公网 IP 或正式生产可用性认证。

文档处理任务和导出任务使用原生 Asynq；导出进入 maintenance 队列并额外限制转换并发，不占用上传核心队列。没有另建任务平台。默认 worker 并发适合本机验证；生产根据同机模型/解析压力调整现有 `WEKNORA_ASYNQ_*` 配置即可。

原生 macOS 客户端、录音设备、飞书双向同步、深度视频检索、完整任务管理和主动 Agent 未包含在首版。

## 一致性备份与独立恢复

备份会短暂停止**当前项目**的 app 和 Redis，使用 `pg_dump`、文件卷与 Redis AOF 一致归档；结束自动重启。备份包含 `.env`，请按凭据保管，不要提交 Git 或公开上传。

```sh
python3 scripts/researchctl.py backup /绝对路径/新备份目录
python3 scripts/researchctl.py verify-backup /绝对路径/备份目录
python3 scripts/researchctl.py restore /绝对路径/备份目录 --project context-restore-check --port 8092
```

恢复只允许**全新的项目名**，拒绝覆盖已有容器或数据卷；归档校验包含 SHA-256 和路径检查。备份记录运行中镜像的确切 ID，恢复时使用这些二进制，不跟随可能已重建的本机标签；跨主机恢复需先载入匹配镜像。恢复实例使用独立命名卷，不影响原库。定期实际恢复并核对原件、研究稿版本和下载，比只保存数据库文件更可靠。

## 验证与限制

**当前是完成本机核心流程验证的版本，完整生产上线验收尚未通过。** 详细结果、实测证据、测试失败对照与待完成门槛见 [验收记录](docs/RESEARCH_ACCEPTANCE.md)。

2026-10-22 补充：真实 DeepSeek、整库语义检索、多轮修订与经验复用的结果见[模型与 Langfuse 功能验收](docs/RESEARCH_SEMANTIC_LANGFUSE_ACCEPTANCE_2026-10-22.md)。最新[团队容量与资源隔离验收](docs/RESEARCH_TEAM_CAPACITY_2026-10-22.md)完成 5 个固定混合工作流的 10 分钟持续负载与 40 秒 Langfuse 中断恢复；699 项操作全部成功，但完整论文入库 11 分 12 秒，未达到 10 分钟目标。推荐先按该负载组合试运行，不能视为全天稳定性或任意人数承诺。下文的无模型、合成数据等说明描述较早验收阶段，应结合上述有日期的补充报告阅读。

已验证私密收件箱、精确共享受众与撤权、原生同 ID 移动、并发重导去重、历史原件保全、研究稿冲突与回滚、旧导出实时权限、三格式导出和 ZIP 离线读取。最新研究稿流程 14 项、来源资产 3 项真实 API 检查通过；导出的 Word/PDF 经原生解析器重新导入后仍可读出中文标题和表格内容。

以下脚本使用仅存于忽略文件 `.research-state.json` 的本机测试账号，不打印凭据。请在隔离验收环境**顺序运行**；权限检查会暂时调整测试组织成员和共享关系后恢复：

```sh
python3 scripts/research-qa-setup.py
python3 scripts/research-live-acceptance.py --module capture
python3 scripts/research-live-acceptance.py --module native
python3 scripts/research-live-acceptance.py --module originals
python3 scripts/research-live-acceptance.py --module dedup
python3 scripts/research-drafts-acceptance.py --module drafts
python3 scripts/research-drafts-acceptance.py --module source-assets
python3 scripts/research-document-acceptance.py
```

原版基线通过 `python3 scripts/researchctl.py up --baseline` 启动，使用同一五服务配置和固定的官方镜像。基线二进制不认识定制 API；已有定制数据时不要用原版编辑正式研究稿。性能对照应使用同一备份恢复出的隔离实例与同等负载，并在实际切换二进制后分别测量：

```sh
python3 scripts/research-benchmark.py --label baseline --base-url http://localhost:8092 --series restore-matched --samples 500
python3 scripts/research-benchmark.py --label custom --base-url http://localhost:8092 --series restore-matched --samples 500
```

`--label` 只记录结果类别，不会自动切换版本。最终同一恢复数据上的复测，每入口三轮、每轮 500 次请求：知识列表关键词过滤 P95 为 2.855 → 2.552 ms，全局标题检索为 5.282 → 4.987 ms；这两个入口本轮均未超过 +10%。早期未匹配完整数据库的测量出现过全局标题检索 +33.6%，保留原报告，不作为最终验收比较。该脚本仅测元数据/标题检索，不包含模型调用，**不代表通用加速或向量混合检索达到 P95 门槛**；完整数据见 [验收记录](docs/RESEARCH_ACCEPTANCE.md#性能)。

报告、导出预览和性能数据在 `artifacts/research/`，默认不提交。50 份验收文档是明确标记的**合成草稿**，用于验证持久化、权限和开放导出。本轮未配置实际聊天/Embedding 模型，也未完成外部内容网络的连通性验收；不能代替真实公众号、论文、扫描件的解析和 AI 引用效果验收。

Go 定制专项测试通过；本轮完整运行的业务相关包套件仍有 44 项失败，与固定版本原版在相同环境中的失败集合一致，没有新增失败项，不能称该套件通过，也没有宣称全仓 Go 测试已全部完成。最终文档镜像的 Python 专项 21 项通过。前端类型检查、生产构建和聚焦回归通过，但 npm 审计仍有剩余风险，详见 [安全复核](docs/RESEARCH_SECURITY.md)。

`.research-backups/final-release/` 已恢复到独立项目 `context-restore-verify`，七项核对通过：校验和、五服务镜像、50 份资料、历史原件、研究稿版本与修订、历史导出下载、ZIP 离线正文。恢复实例已在验收后停止，主平台五服务保持运行。报告见 [backup-restore-report.json](artifacts/research/backup-restore-report.json)；恢复实例运行时可用以下只读命令复核：

```sh
python3 scripts/research-restore-acceptance.py --backup .research-backups/final-release \
  --base-url http://localhost:8092 --project context-restore-verify
```

上线前仍需使用团队真实的 50 份代表性资料、实际聊天/Embedding 模型、同硬件原版基线完成：完整收集、同 ID 移动后的索引/图谱/引用一致性、引用正确率、混合检索 P95 增幅 ≤10% 和并发导出下上传成功率，并在实际部署环境复核备份恢复。缺模型或真实样本时应标为待验收或 blocked，不能用伪模型或直接修改数据库把任务改成成功。

新增组件必须说明它替代的现有环节、重复状态、回退方式及实测收益。原件、来源版本、Markdown/JSON 导出和原生接口是后续更换模型、解析器和索引的边界；升级能力不依赖持续叠加框架。
