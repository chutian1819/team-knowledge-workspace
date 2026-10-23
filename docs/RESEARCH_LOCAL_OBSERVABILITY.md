# 本机语义检索与 Langfuse 运维说明

这套配置用于本机研究验收。平台界面为 `http://localhost:8090`，Langfuse 为 `http://localhost:3000`。入口目前仅绑定本机回环地址，尚未开放团队远程访问。

## 职责与数据边界

| 部分 | 职责 | 持久化位置 |
| --- | --- | --- |
| 研究平台 | 原件、正文分块、向量索引、稿件版本、修订事件、写作经验及确认状态 | 原平台 PostgreSQL 与文件卷 |
| 本地 Ollama / EmbeddingGemma | 把片段和问题转换为 768 维向量 | 仅模型权重卷；知识向量仍写入原平台 |
| DeepSeek | 根据检索证据生成和修订稿件、提炼候选写作经验 | 结果与历史写回平台；保留原 Kimi 配置 |
| Langfuse | 记录检索、模型调用、耗时、用量和学习流程关联 | 独立 PostgreSQL 数据库、ClickHouse、MinIO 与 Redis |

Langfuse 的 `embedding` 是观测类型，不负责取代知识库。写作经验的候选、确认、复用和撤销都是平台业务逻辑。Langfuse 通过异步 OTLP 旁路接收记录，关闭或故障不应回滚业务保存。参考 [官方观测类型](https://langfuse.com/docs/observability/features/observation-types)。

PostgreSQL 实例复用，但 Langfuse 使用独立 `research_langfuse` 数据库和受限角色，无权读写平台业务表。Redis 使用独立 `research-langfuse-redis` 容器、密码、卷和 `noeviction` 策略；最初试部署的原 Redis DB1 内容保留，不删除。服务仍共享本机硬件和 Docker 虚拟机，因此不是故障域完全隔离。

## 启动与配置

先启动原平台五个服务，再执行：

```sh
python3 scripts/research-observability-setup.py
docker compose --env-file .research-observability.env -f compose.research.observability.yaml up -d
python3 scripts/research-embedding-bootstrap.py
python3 scripts/research-observability-setup.py --enable-platform
docker compose -f compose.research.yaml up -d --no-deps app
```

原平台已存在，因此不要重新初始化业务数据库。首次配置生成的 `.research-observability.env` 为 0600 私有文件，包含项目凭据、加密密钥和管理员登录信息；不得提交到 Git 或放入 Docker 构建上下文。数据库已存在而私有文件丢失时，脚本会拒绝生成替代密钥，应先恢复原文件。

已有的共享 Redis 配置只能显式执行 `--migrate-redis`。这仅迁移本机服务配置，保留旧 DB1；不是运行中生产队列的数据迁移工具。

原生本地 Embedding 适配器使用管理员的 `OLLAMA_BASE_URL=http://research-embedding:11434`，模型设置中的本地模型无需填写单独 Base URL。`research-enable-semantic-index.py` 专用于现有验收账户/资料库，不能视为任意团队资料的迁移脚本。

Ollama 0.40.8 在本机透明代理的 fake-IP DNS 下拉取权重遇到重定向校验失败。bootstrap 脚本通过宿主机从官方 HTTPS registry 获取固定权重，逐个检查长度及 SHA-256，再导入模型卷；没有关闭 Ollama 的地址校验。首次索引在容器 CPU 上计算，持续时间不代表日常单次检索延迟。Mac GPU 加速和多人并发容量尚未验收。

## 停止、备份与恢复边界

单独停止观测服务不会删除数据：

```sh
docker compose --env-file .research-observability.env -f compose.research.observability.yaml stop langfuse-web langfuse-worker clickhouse minio langfuse-redis
```

保留本地模型服务即可继续平台语义检索。恢复观测服务用同一 compose 的 `up -d`。本机不需要时可将平台 `.env` 中 `LANGFUSE_ENABLED=false` 并重新创建 app；不要删除业务记录或向量索引。

必须分别备份业务与观测数据：

```sh
python3 scripts/researchctl.py backup .research-backups/business-UNIQUE-NAME
python3 scripts/research-observability-backup.py .research-backups/langfuse-UNIQUE-NAME
```

业务备份包含平台数据库、原件/导出、原 Redis 和 `.env`，不包含新增 Langfuse 数据。观测备份暂停其写入者及存储，保存独立数据库、ClickHouse、MinIO、独立 Redis、私有配置和镜像标识，并恢复之前运行的确切容器；检查 SHA-256、tar 可读性和完整 gzip CRC。两份备份各自一致，不构成跨系统原子快照。备份文件包含凭据与研究内容，必须保留私有权限。

当前补充脚本不执行恢复，也不覆盖现有数据。跨主机恢复需要相应版本的容器、独立角色、卷与网络，随后重新绑定平台的服务地址。没有完成 Langfuse 灾难恢复演练，不能把校验和通过称为恢复演练通过。参考 [官方备份说明](https://langfuse.com/self-hosting/configuration/backups)。

## 作为团队服务器前

本机部署具备功能验证条件，但仍需另行完成稳定域名/HTTPS、成员独立账号与权限、远程访问策略、备份恢复演练、追踪保留期与磁盘告警，以及实际团队并发压测。不要直接把全部容器端口开放到公网。

本次实际遇到 Docker 虚拟机 60 GB 磁盘满，即使 macOS 宿主机仍有剩余空间；已通过清理可重新生成的构建缓存恢复，没有删除资料卷。容量检查必须同时查看宿主机和 Docker 内部文件系统。记录镜像、模型、构建缓存和业务数据的独立增长，不能仅靠容器内存上限防止整机资源耗尽。

另一次批量观测复验与前后端构建重叠时，应用发生 OTLP 导出超时，Langfuse 队列出现 socket timeout；资料入库和索引完成，但部分观测未成功到达。构建结束后的稳定复测必须与这次失败分别记录。异步观测是尽力交付，不能替代平台数据库中的业务审计历史，也不能承诺负载峰值下零日志丢失。团队环境应将构建工作移出服务主机或明确限制其资源，并为观测导出失败设置告警。

固定镜像摘要见 `compose.research.observability.yaml`。服务版本为 Langfuse 4.67.0、Ollama 0.40.8；本机资源限制为验收值，不是 [Langfuse 生产部署容量建议](https://langfuse.com/self-hosting/deployment/docker-compose) 的等价实现。
