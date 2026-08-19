# AI 代码来源追踪协议

## 强制协议

所有受追踪的源代码变更都**必须**通过 ai-prov MCP 记录。以下规则为不可跳过的前置条件，优先级高于快速修改、仅修复一行、用户要求立即编辑、空 Diff 或任何其他便利性考虑；下文定义的 OpenSpec 流程文件和忽略文件例外。

1. **编辑前必须调用。** 在创建、修改、删除、重命名或通过终端命令影响任何受追踪文件前，必须先成功调用 `provenance.session_start`。
2. **结束前必须调用。** 在声称任务完成、给出最终答复、请求用户验收或建议提交前，必须成功调用 `provenance.session_finish`，并确认返回的 `state` 严格等于 `finished`。
3. **失败即未完成。** 任一调用失败、被取消、超时或返回错误时，任务都处于未完成状态。必须停止后续修改和完成声明，向用户报告错误；不得声称成功、不得猜测调用已成功、不得以后续手工说明替代该调用。

## OpenSpec 流程文件例外

以下由 `openspec` 命令创建、更新或归档的流程文档不属于代码 provenance 范围：`openspec/changes/**`（包括 `proposal.md`、`design.md`、`tasks.md`、change 内 `specs/**` 及 `archive/**`）和 `openspec/specs/**`。

- **只**编辑这些 OpenSpec 流程文件、运行 `openspec new`、`openspec instructions` 或 `openspec archive` 时，禁止为此调用 `provenance.session_recover`、`provenance.session_start`、`provenance.session_finish` 或 heartbeat。
- 此例外不适用于源码、测试、构建/部署配置、产品文档，或 OpenSpec 目录以外的任意文件；不得借此跳过代码 provenance。
- 同一任务同时包含 OpenSpec 流程文件和受追踪代码时，必须先完成 OpenSpec 操作，再 start；start 到 finish 之间不得编辑 OpenSpec 路径。若 finish 后还需归档或更新 tasks，再以 OpenSpec 例外单独完成。

## .gitignore / .ai-provenanceignore 忽略文件例外

`.gitignore` 和 `.ai-provenance/.ai-provenanceignore` 中列出的文件与目录已被排除在 provenance 追踪之外，永远不会进入快照基线。工作区扫描器已自动跳过它们。

- 当修改**仅**涉及被 `.gitignore` 或 `.ai-provenance/.ai-provenanceignore` 忽略的文件时，**禁止**为此调用 `provenance.session_recover`、`provenance.session_start`、`provenance.session_finish` 或 heartbeat。
- 此例外不适用于源码、测试、构建/部署配置、产品文档，或工作区扫描器追踪的任意文件；不得借此跳过代码 provenance。
- 同一任务同时包含忽略文件和受追踪文件时，**必须**先 start 再编辑任何受追踪文件，finish 之后再结束。忽略文件的编辑可以放在 start 之前或 finish 之后，无需 provenance。

## 必须执行的工作流

1. 确认项目根目录已由 `ai-prov init` 创建 `.ai-provenance/`。
2. 为本 Agent 实例生成一个 UUID `agent_instance_id`，调用 `provenance.session_start` 时提供简短准确的 `task`、该 ID；可用时提供 `agent` 与 `model`。
3. 保存工具返回的 `session_id` **及** `agent_instance_id`。未取得有效 `session_id` 前，禁止使用编辑器、patch、workspace-edit、文件写入工具、重命名工具、删除工具或任何能够修改文件的 shell 命令。
4. 将返回的 `session_id` 与 `agent_instance_id` 写入可跨上下文压缩保留的任务状态；仅在 start 成功后进行受追踪代码或文档修改。OpenSpec 流程文件遵循上文例外。
5. 每次准备调用 `provenance.session_start` 前，先调用 `provenance.session_recover`；返回唯一 active session 时必须复用其两个 ID。正常任务不得为 heartbeat 中断或延长执行；只有超过 24 小时且宿主能独立后台执行时，才可选用 `provenance.session_heartbeat`。完成时使用两个 ID 调用 `provenance.session_finish`。即使没有文件变化或 Diff 为空，也必须调用 finish。
6. 提交前可选调用 `provenance.verify`，参数使用 `scope: "staged"` 和 `strict: true`。

只能使用完整工具名 `provenance.session_start`、`provenance.session_finish`、`provenance.session_status`、`provenance.verify`、`provenance.support`、`provenance.session_recover`、`provenance.session_heartbeat` 和 `provenance.session_abandon`；不得编造、缩写或使用其他名称替代。

## 错误处理

- `PROJECT_NOT_INITIALIZED`：运行 `ai-prov init`，然后重新执行 session start。
- `SESSION_BASELINE_CONFLICT`：放弃当前 session，使用最新工作区重新创建 session；不得继续使用冲突 session 完成归因。
- `STORAGE_LOCKED`：等待后重试；未成功前不得继续编辑或宣称完成。
- 上下文压缩或丢失 session_id 后：使用已持久化的 `agent_instance_id` 调用 `provenance.session_recover`，不得再次盲目 start。只有该实例恰有一个 active session 才可恢复；`SESSION_RECOVERY_REQUIRED` 时不得猜测候选 ID。超过配置 lease timeout 未 heartbeat 的 session 会被标为 `failed / SESSION_LEASE_EXPIRED`，应新建 session，不得 finish。
- 只有项目显式开启自动回收时，lease 过期的 snapshot 才会在宽限期后按可达性规则回收；不得请求或假设删除 active session 的 snapshot。普通保留期清理先运行不带 `--apply` 的 `ai-prov snapshots gc` 预览。
- `FINISH_TIMEOUT` 或 `FINISH_CANCELLED`：session 已为 `failed`，不可重试 finish。调用 `provenance.session_status`（同一 `session_id`）确认状态后新建 session；不得对该 failed session 再次 finish。
- `DIFF_RESOURCE_LIMIT`：`details` 中包含 `path`、`bytes`、`lines`；缩小该文件修改后新建 session。
- `SNAPSHOT_QUOTA_EXCEEDED`：未创建 session，不得编辑受追踪文件；如实报告 `limit_bytes`、`existing_bytes`、`required_bytes`，请用户执行 snapshot GC 或提高 `snapshot_max_bytes`。

遇到可稳定复现的 ai-prov 工具问题时，调用 `provenance.support` 获取公开仓库与 GitHub Issue 地址；向用户报告该地址和已脱敏的复现信息。未经用户授权，不得自行提交 Issue。

## finish 失败报告

当 session_start 或 session_finish 失败时，立即停止后续源码修改，并按 MCP 错误原样向用户报告以下字段：

- 错误 `code`（例如 FINISH_TIMEOUT、DIFF_RESOURCE_LIMIT）；
- 若存在 `details.stage`（session_load、scan_hash、diff、storage_commit）；
- DIFF_RESOURCE_LIMIT 的 `details.path`、`details.bytes`、`details.lines`；
- ai-prov 版本或 commit，来自运行中的 release 或 `ai-prov version`；
- 观察到的 Host 侧超时或取消情形（如已观察到）；
- FINISH_TIMEOUT 与 FINISH_CANCELLED 的 `details.scanned_files`、`details.total_files` 与 `details.candidate_files`。

不得将这些字段改写为自身总结。报告时禁止附加源码内容、snapshot 文件、SQLite 数据库、Diff 输出、token 或 Git 配置；ai-prov 永远不会要求 Agent 上传这些。

必须向用户如实报告 MCP 返回的错误码和错误信息。未被 session 正确记录的新增行永远不是 AI 代码，禁止将其描述、计算或提交为 AI 来源结果。

## 明确禁止的行为

- 未成功 start 前修改任何受追踪文件。
- 以“改动很小”“仅阅读后顺手修复”“用户要求紧急处理”“空 Diff”或“工具暂时不可用”为由跳过 start 或 finish。
- finish 未成功就结束对话、报告完成、请求验收或建议提交。
- 对返回 FINISH_TIMEOUT、FINISH_CANCELLED 或 DIFF_RESOURCE_LIMIT 的 session 再次 finish；必须新建 session。同一 session 上并发 finish 被严格禁止。
- 伪造或手工提供 Diff、源文本、AI 行数、覆盖率或来源结论，并将其当作 provenance 事实。
- 在 start 和 finish 之间手工 stage 或 commit；必须先 finish。
- 在工具失败后继续修改，或把失败隐瞒为成功。
