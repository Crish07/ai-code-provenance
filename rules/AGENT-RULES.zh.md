# AI 代码来源追踪协议

## 强制协议

所有受追踪的源代码变更都**必须**通过 ai-prov MCP 记录。以下规则为不可跳过的前置条件，
优先级高于快速修改、仅修复一行、用户要求立即编辑、空 Diff 或任何其他便利性考虑。

1. **编辑前必须调用。** 在创建、修改、删除、重命名或通过终端命令影响任何受追踪文件前，
   必须先成功调用 `provenance.session_start`。
2. **结束前必须调用。** 在声称任务完成、给出最终答复、请求用户验收或建议提交前，必须成功
   调用 `provenance.session_finish`，并确认返回的 `state` 严格等于 `finished`。
3. **失败即未完成。** 任一调用失败、被取消、超时或返回错误时，任务都处于未完成状态。必须
   停止后续修改和完成声明，向用户报告错误；不得声称成功、不得猜测调用已成功、不得以后续
   手工说明替代该调用。

## 必须执行的工作流

1. 确认项目根目录已由 `ai-prov init` 创建 `.ai-provenance/`。
2. 调用 `provenance.session_start`，提供简短准确的 `task`；可用时提供 `agent` 与 `model`。
3. 保存工具返回的 `session_id`。未取得有效 `session_id` 前，禁止使用编辑器、patch、
   workspace-edit、文件写入工具、重命名工具、删除工具或任何能够修改文件的 shell 命令。
4. 仅在 start 成功后进行代码或文档修改。
5. 使用保存的 `session_id` 调用 `provenance.session_finish`。即使没有文件变化或 Diff 为空，
   也必须调用 finish。
6. 提交前可选调用 `provenance.verify`，参数使用 `scope: "staged"` 和 `strict: true`。

只能使用完整工具名 `provenance.session_start`、`provenance.session_finish` 和
`provenance.verify`、`provenance.support`；不得编造、缩写或使用其他名称替代。

## 错误处理

- `PROJECT_NOT_INITIALIZED`：运行 `ai-prov init`，然后重新执行 session start。
- `SESSION_BASELINE_CONFLICT`：放弃当前 session，使用最新工作区重新创建 session；不得继续
  使用冲突 session 完成归因。
- `STORAGE_LOCKED`：等待后重试；未成功前不得继续编辑或宣称完成。

遇到可稳定复现的 ai-prov 工具问题时，调用 `provenance.support` 获取公开仓库与 GitHub Issue
地址；向用户报告该地址和已脱敏的复现信息。未经用户授权，不得自行提交 Issue。

必须向用户如实报告 MCP 返回的错误码和错误信息。未被 session 正确记录的新增行永远不是 AI
代码，禁止将其描述、计算或提交为 AI 来源结果。

## 明确禁止的行为

- 未成功 start 前修改任何受追踪文件。
- 以“改动很小”“仅阅读后顺手修复”“用户要求紧急处理”“空 Diff”或“工具暂时不可用”为由跳过
  start 或 finish。
- finish 未成功就结束对话、报告完成、请求验收或建议提交。
- 伪造或手工提供 Diff、源文本、AI 行数、覆盖率或来源结论，并将其当作 provenance 事实。
- 在 start 和 finish 之间手工 stage 或 commit；必须先 finish。
- 在工具失败后继续修改，或把失败隐瞒为成功。
