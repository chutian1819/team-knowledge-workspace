# 团队容量实测工具

`research-capacity-test.py` 运行真实平台请求，不模拟模型。不修改原有 220 个分块或旧稿；检索只读原资料库，写作在新建研究库的独立槽位稿件上使用冻结的 8 个真实检索片段，上传只进入新建测试库。全部候选稿保持待审，不接受、不抽取或应用个人偏好。每轮写作要求 600–900 字，同时记录实际输出长度。

## 首次准备与单级基线

以下路径可替换；后续始终使用同一个 `--output`、`--run-label` 以复用独立资料和累计预算。

```sh
# 离线计划：不会登录或发送平台请求
python3 scripts/research-capacity-test.py plan --kind search --max-concurrency 1 --operations 40

# 建立两个独立测试库、8篇槽位稿件，冻结原库检索证据；0次DeepSeek
# 此步骤会产生8次查询Embedding，不产生任何上传索引后台任务。
python3 scripts/research-capacity-test.py prepare \
  --output artifacts/research/capacity-2026-10-22/workload \
  --run-label team-capacity --max-concurrency 8

# 只跑1并发40次真实语义查询；不会自动进入2/4/8
python3 scripts/research-capacity-test.py stage \
  --output artifacts/research/capacity-2026-10-22/workload \
  --run-label team-capacity --trial before-v2 --kind search --level 1 --operations 40

# 只跑1并发混合工作：4次检索、2次写作、1次真实上传
python3 scripts/research-capacity-test.py stage \
  --output artifacts/research/capacity-2026-10-22/workload \
  --run-label team-capacity --trial before --kind mixed --level 1 --operations 7

# 独立测试8个同时发起的写作工作流；不把数据库模型max_concurrency数字当作同步写作上限
python3 scripts/research-capacity-test.py stage \
  --output artifacts/research/capacity-2026-10-22/workload \
  --run-label team-capacity --trial writing8 --kind writing --level 8 --operations 8
```

`--trial` 标识一次实验阶段；同一阶段已完成的操作不重复提交。修改配置后使用新 trial，例如 `after`；原报告和每次请求记录仍保留在阶段数据中。`run` 才会按 `--levels 1,2,4,8` 自动逐阶执行，任一停止条件触发后不会进入更高阶。筛查默认混合权重 `4,2,1`，每阶 `7×并发` 项，整套共 60 次检索、30 次 DeepSeek 候选稿、15 次上传。检索单项默认每阶 40 次。`--operations` 是每阶操作数，`--repeats` 是该数量的倍率。

## 截止、预算与稳态

- 最大线程数由 `--max-concurrency` 限定，默认 8，允许上限 32。实际运行槽位由 `--level` 决定；每个槽位最多持有一个客户端工作流。每项操作运行在可被强制结束的独立子进程中，凭据只通过内存管道传递。
- `--operation-timeout 240`、`--stage-timeout 900`、`--total-timeout 3600` 给出客户端操作、阶段、当次命令的截止，清理子进程另容许约 1 秒。不宣称这会撤销服务器已经接受的后台任务。
- 默认累计 `--max-writing-calls 32`；操作开始前先预留预算，失败、中断不退还。允许明确传入上限 128。Embedding 默认累计预留上限 1024；查询预留 1，上传按最多 12 块、每块一次请求及后台最多 4 次尝试预留 48。实际批量 Embedding 通常更少，但这不是服务端计费硬阀。若真实分块超出预留即停止新工作。
- 遇到第 1 次 429、第 1 次超时或未知后台结果停止增压；完成至少 8 项后失败率达到 10% 停止。阶段结束也检查失败率和端到端 P95，默认 P95 截止 180 秒。阈值均可显式设置；已在途操作收尾并保留结果。
- 10 分钟连续负载需明确提供 `--duration-seconds 600 --operations N`；到数量、预算、时间任一上限停止发起新操作，并等待在途操作按各自期限完成。`--stage-timeout` 必须为收尾留出余量。不会自动耗尽剩余写作预算。例如检索临界级可显式增加操作上限；写作持续测试应先查看预算，再决定是否提高 `--max-writing-calls`。
- `--lanes search,search,writing,writing,upload` 固定 5 个槽位的任务类型，需配合 `--kind mixed --level 5 --duration-seconds`。`--large-upload-first` 令该阶段首个上传使用完整原论文，其余仍为小样本；应显式提高分块预算与单项期限，并单列完整论文结果。

本次已执行的持续负载命令（重复实验应另取 `--trial`；会调用付费模型）：

```sh
python3 scripts/research-capacity-test.py stage \
  --output artifacts/research/capacity-2026-10-22/workload \
  --run-label team-capacity --trial steady --kind mixed --level 5 \
  --lanes search,search,writing,writing,upload \
  --duration-seconds 600 --operations 2500 --large-upload-first \
  --max-upload-chunks 64 --operation-timeout 900 \
  --stage-timeout 1200 --total-timeout 1800 \
  --max-writing-calls 128 --max-embedding-calls 10000
```

## 报告与恢复

```sh
# 离线重算统计，不请求平台
python3 scripts/research-capacity-test.py report \
  --output artifacts/research/capacity-2026-10-22/workload --run-label team-capacity

# 对超时写作/已知上传ID仅做只读结果核查；不重发，不修改原失败统计
python3 scripts/research-capacity-test.py report --reconcile \
  --output artifacts/research/capacity-2026-10-22/workload --run-label team-capacity

# 读取真实Langfuse观测；网络查询分页有界，不制造观测记录
python3 scripts/research-capacity-test.py report --langfuse \
  --output artifacts/research/capacity-2026-10-22/workload --run-label team-capacity

# 离线工具机械回归；没有真实平台请求，也不伪造模型验收
python3 -m unittest discover -s scripts -p test_research_capacity_test.py -v
```

输出中的 `fixtures.json` 持久化独立库/稿件、阶段、操作ID及累计预算。`operations/<id>/requests.jsonl` 逐次保存请求开始/结束、HTTP状态、延迟、请求ID、trace ID及是否为主请求；正文和鉴权信息不进入该日志。每个写作候选稿另以权限 0600 保存供复核。失败提案仍保留在平台中，不自动清理。已记录的成功、失败和超时均不重试；未知写入先做只读核验，无法确认时停止，不擅自创建重复数据。

报告区分主请求延迟、上传到分块索引就绪的完整延迟以及轮询开销，列出按类型成功率、状态分布、P50/P95、处理字数/字节/页数/块数和吞吐。P95 使用 nearest-rank；样本不足 40 明确提示。超时样本在完整分布中保留，成功样本分布单列。实际活跃并发按操作的开始/结束时间计算峰值和时间加权均值；它包括等待索引的客户端工作流，不等于活跃HTTP连接、服务器推理并发或注册用户数。

语义问题有 80 个真实主题/角度组合，超过后循环，重复情况有标记；`--cache-mode repeat` 专门测重复请求。准备已预热 Embedding；没有清理缓存，也不声称第一次请求必然冷启动。上传使用既有 `pi0.pdf` 的前两页，保留原件哈希和页数；文本为该真实PDF的逐字片段。每份PDF只更改测试副本的元数据以避免文件去重跳过解析，每份文本添加明确测试身份；全部关闭摘要、图谱、问题生成和视觉推理，测量文字解析与索引路径。

Langfuse 报告计算准确的已提交 HTTP trace 送达比例，并核对每种成功工作流必需的最小观测链（HTTP→Embedding、HTTP→研究提案→Chat、HTTP→后台解析→Embedding）。后台分批和重试数量并非预先精确已知，因此仅将其标记为“必需观测覆盖率”，不伪称所有 observation 的精确送达率。最初请求中断、trace 尚未送达、分页失败分别保留原状。

这是一组有界、闭环客户端负载数据。短筛查不能单独证明长期稳定容量，不能据此承诺固定团队人数；需结合持续负载、服务器资源、后台排队和实际用户工作节奏判断。
