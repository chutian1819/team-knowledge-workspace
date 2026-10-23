# 前端依赖安全复核

复核日期：2026-10-20。范围：当前 `frontend/package-lock.json`、浏览器调用路径和静态 Nginx 镜像构建；不是完整渗透测试，也不覆盖 Go、Python、LibreOffice 或容器 OS。**锁定 WeKnora 版本只能保证可复现，不能视为安全通过。**

## 本次结论与最小修补

只读 `npm audit --json` 最初报告 14 个受影响包条目：1 critical、8 high、3 moderate、2 low。这包含传递依赖和重复归因，不等于 14 个独立漏洞。

确认原生聊天 Mermaid 代码块、上传 `.mmd` 原件预览会解析不可信图表。`mermaidShared.ts` 的 `securityLevel: strict` 以及生成 SVG 后再清洗，不能阻止解析过程的原型污染或死循环。新增研究稿预览将 Mermaid 当普通代码展示，但研究工作台复用原生引用预览和聊天，因此仍需修补。

已将 **Mermaid 11.15.0 → 精确锁定 11.16.1**。这是官方对以下五条直接漏洞的共同修复版本：

| 漏洞 | 适用判断 |
| --- | --- |
| [Architecture 原型污染](https://github.com/mermaid-js/mermaid/security/advisories/GHSA-3rrr-jr9j-h3q3) | 不可信图表解析路径可达，必须修补 |
| [XY 相等范围死循环](https://github.com/mermaid-js/mermaid/security/advisories/GHSA-2v8p-3f2j-5mp7) | 不可信图表解析路径可达，必须修补 |
| [Radar 超大 ticks 阻塞](https://github.com/mermaid-js/mermaid/security/advisories/GHSA-rhh3-jpg6-66xh) | 不可信图表路径可达，必须修补 |
| [CSS 越界影响相邻元素](https://github.com/mermaid-js/mermaid/security/advisories/GHSA-6x64-9x62-f2gx) | 当前独立容器降低风险，一并修复，防止后续容器布局变化暴露 |
| [配置 API 原型污染](https://github.com/mermaid-js/mermaid/security/advisories/GHSA-c4c3-pg64-4m4v) | 当前初始化配置由代码生成，未发现攻击者控制配置 API 的入口，一并修复 |

新 Mermaid 的最低依赖范围要求同步更新三个既有传递依赖：`@mermaid-js/parser 1.1.1 → 1.2.1`、`cytoscape 3.33.2 → 3.34.4`、`dayjs 1.11.19 → 1.11.23`。除此之外锁定版本未变；没有新增服务、独立解析器或框架，也没有执行 `npm audit fix --force`。

修补后五条 Mermaid 直接 advisory 已消失。审计仍为 **14 个包条目：1 critical、8 high、2 moderate、3 low**；Mermaid 仍因 KaTeX 传递依赖被标为 low。这次修补不代表全依赖清零，也不主张 11.16.1 修复 KaTeX。

## 剩余风险与当前运行路径

| 依赖及当前版本 | 已核查路径及处理要求 |
| --- | --- |
| `shell-quote 1.10.0`，critical | 经开发依赖 `npm-run-all2` 使用；[公告](https://github.com/advisories/GHSA-pqg4-j6r4-53mv)要求特殊 comment token 与含换行的不可信输入进入 shell 引号流程。生产镜像仅从 builder 拷贝 `dist`，浏览器没有 shell-quote 调用。当前 build 是固定的 `vite build`，没有发现研究页面到此命令注入的路径。仍保留构建链风险，后续在独立变更中修到 ≥1.11.0。 |
| `axios 1.16.0`，high | 浏览器 API 客户端确实使用。Node HTTP/2、代理、DNS 等公告没有当前浏览器适配器路径；formToJSON 等递归接口未发现应用调用，文件表单字段由代码构造。但[继承请求属性](https://github.com/advisories/GHSA-9fr6-4gfg-395g)、[fetch adapter gadget](https://github.com/advisories/GHSA-vh66-26gq-q6x8)要求独立原型污染前提，不能仅因是浏览器就忽略。后续小范围升级到覆盖全部当前公告的版本，并回归认证、上传、超时、下载。 |
| `dompurify 3.4.11`，moderate | 属于实际 Markdown/SVG 清洗边界。[当前公告](https://github.com/advisories/GHSA-6688-9rhm-gjv2)涉及 `IN_PLACE`，另一[公告](https://github.com/advisories/GHSA-c2j3-45gr-mqc4)涉及 `CUSTOM_ELEMENT_HANDLING`；现有调用传入字符串，两配置均未启用。未发现这些公告的完整触发路径，仍需后续针对清洗器的补丁升级与恶意 HTML 回归。 |
| `katex 0.16.45` 及 `marked-katex-extension`、`mermaid`，low | 公式渲染实际使用；[公告](https://github.com/advisories/GHSA-238p-pmpm-9mq7)要求已有原型污染，可能绕过 trust 限制。此次修复一个已知污染入口，不能证明所有前置入口不存在。上游安全版本跨当前依赖范围，须单独验证兼容性，不能强制全树升级。 |
| `vue/@vue/server-renderer 3.5.34`，high | [公告](https://github.com/advisories/GHSA-g2v6-rqmx-r4w6)为 SSR 属性名 XSS。当前生产入口是浏览器 SPA；SSR 调用仅见测试，未发现生产 SSR 服务。若启用 SSR，必须先成组升级 Vue 相关包，重新核查边界。 |
| `fast-uri 3.1.3`、`browserslist 4.28.1`、`baseline-browser-mapping 2.10.0` | 分别经 webpack/Ajv、构建目标计算进入构建链，未发现用户上传数据直接进入其 URI 校验或浏览器查询接口。限制构建输入为受审查代码；不能将这些包用于服务端 URL 安全判断。 |
| `postcss 8.5.14`、`nanoid 3.3.11`、`source-map-js 1.2.1` | 经样式/编译工具链使用；[PostCSS](https://github.com/advisories/GHSA-fxqj-rqcc-2cmp)涉及不可信 sourceMappingURL，[source-map-js](https://github.com/advisories/GHSA-68fv-2mgg-jv7q)涉及恶意 source map；未发现上传文件进入应用编译/source-map 消费接口，也未发现用户控制 nanoid 长度的调用。构建不可信分支仍需隔离。 |

部分构建工具被上游放在 `dependencies`，不能单凭 `npm audit --omit=dev` 判定运行时可达性。上述“未发现路径”是基于当前代码的有限结论，不能当作漏洞不存在。

## 可执行的发布门槛

1. 镜像必须使用本次锁文件构建，保留审计输出与产物摘要；运行容器只提供静态文件，不携带构建 `node_modules`，不公开 Vite 开发服务器。
2. 构建隔离于生产数据和凭据；只执行受审查的包脚本，不把资料内容、用户输入或不可信 PR 参数传入 `npm-run-all`/shell。首次外网发布前，为剩余依赖逐项记录补丁计划或有负责人和到期时间的风险接受，不能只写“固定了上游”。
3. 本次回归包含 Mermaid 三条实际解析安全用例（独立子进程及超时）、原生 Markdown/流式 Mermaid、清洗配置、研究上下文与引用测试。类型检查和生产构建必须通过；浏览器验证正常图表、公式、原件预览、认证后引用和下载。机器测试不能替代浏览器渲染验证。
4. 每次更改渲染配置、启用 SSR、增加 Node 网络代理或让服务器编译用户内容时，重新判断本表中的不可达假设。新增页面继续使用原生权限和清洗器，不能通过 `v-html` 绕过清洗。
5. Go/Python/LibreOffice/镜像 OS 另行扫描；正式上线还需完成方案中的跨租户权限、撤权后引用/导出、备份恢复与 50 份真实资料验收。

回退：若 Mermaid 升级发生兼容性问题，优先修复或禁用原生 Mermaid 渲染入口；如确需回退 package 和 lock，必须同时关闭不可信图表解析再重建镜像，不能带着已知可达漏洞恢复 11.15.0。没有数据迁移，也无需新增服务。

## 复核命令

在 `frontend/` 执行；审计有剩余风险时退出码 1 是预期结果，不要掩盖：

```sh
npm audit --json > audit.json
npm explain shell-quote mermaid katex dompurify axios
npm test -- src/utils/mermaidSecurity.test.ts src/utils/mermaidStreaming.test.ts src/utils/documentPreviewMarkdown.test.ts src/utils/markdownDomPurify.test.mjs src/views/research/workspaceState.test.ts src/views/research/researchCitations.test.ts
npm run type-check
npm run build
```

本次 31 个 focused tests、`vue-tsc --build` 和生产构建均通过，构建仅有既有的大分块提示；安全解析用例验证了拒绝原型父节点、相等 XY 范围及时返回、超大 radar ticks 被限制。没有在运行中的用户浏览器内执行危险 PoC。
