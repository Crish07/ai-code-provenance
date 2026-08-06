# Agent Rules 与 MCP 配置

这些公开模板要求 Coding Agent 在修改受追踪源码前后创建并完成 ai-prov
provenance session。

[English README](README.md)

## 选择模板

| Agent | 源文件 | 目标位置 |
| --- | --- | --- |
| Codex | AGENTS.md | 项目根 AGENTS.md |
| Claude Code | CLAUDE.md | 项目根 CLAUDE.md |
| Cursor | cursor-rules.mdc | .cursor/rules/ai-prov.mdc |
| 任意兼容 Agent | AGENT-RULES.md | 该 Agent 自动加载的指令文件或 Rules 目录 |

未列出的 Agent 使用 AGENT-RULES.md。请复制到该 Agent 会自动加载的文件或
目录中，而不是仅粘贴到一次性的对话提示词里。

## 强制协议

1. 在被追踪项目中一次性运行 ai-prov init。
2. 编辑前调用 provenance.session_start。
3. 使用任意支持的 Agent 能力修改代码。
4. 调用 provenance.session_finish，并确认 finished。
5. 提交前可校验 staged 变更。

start 或 finish 失败表示任务未完成，不能绕过协议。未记录的行绝不会被认为是
AI 代码。

## MCP 配置

所有客户端都需要名为 ai-prov、命令为 ai-prov-mcp 的 stdio server：

~~~json
{
  "mcpServers": {
    "ai-prov": { "command": "ai-prov-mcp" }
  }
}
~~~

只能使用完整工具名 provenance.session_start、provenance.session_finish
和 provenance.verify。stdout 专用于 MCP 协议，诊断写入 stderr。

## 恢复方式

- PROJECT_NOT_INITIALIZED：运行 ai-prov init。
- SESSION_BASELINE_CONFLICT：放弃当前 session 并重新 start。
- STORAGE_LOCKED：稍后重试。

安装和 CLI 命令见项目根 README。
