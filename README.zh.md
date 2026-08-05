# ai-code-provenance

面向 MCP Coding Agent 的本地 AI 代码来源追踪工具。

本项目在本地记录已声明的 AI 编码会话，计算真实工作区变更，并为 Git
变更生成来源覆盖率；不会上传源码、Diff 或项目文件。

[English README](README.md)

## 组件

- ai-prov：初始化、状态、校验和报告 CLI。
- ai-prov-mcp：记录 session 与 provenance 的 stdio MCP server。
- rules：要求兼容 Agent 使用 MCP 的模板。

本地状态保存在 .ai-provenance，请在被追踪项目中忽略该目录。

## 快速开始

~~~sh
make build
cd /path/to/project
ai-prov init
~~~

在 MCP 客户端中配置 command 为 ai-prov-mcp。Codex 复制 rules/AGENTS.md，
Claude Code 复制 rules/CLAUDE.md，Cursor 复制 rules/cursor-rules.mdc。

强制流程：编辑前调用 provenance.session_start；完成修改；调用
provenance.session_finish 并确认 finished；提交前可运行：

~~~sh
ai-prov verify --scope staged --strict
~~~

start 或 finish 失败时，Agent 不得宣称任务完成；未记录新增行是未覆盖行，
绝不会被视为 AI 代码。

## 命令

~~~sh
ai-prov version
ai-prov init
ai-prov status
ai-prov verify --scope staged --strict --json
ai-prov report --scope staged --json
~~~

## MCP 工具

| 工具 | 用途 |
| --- | --- |
| provenance.session_start | 创建 session 和基线 snapshot。 |
| provenance.session_finish | 计算本地 Diff 并保存 provenance。 |
| provenance.verify | 校验 staged 或 worktree 新增行。 |

未初始化时运行 ai-prov init；基线冲突时重新创建 session；存储锁定时稍后重试。

## 开发

~~~sh
make fmt
make test
make test-race
make vet
make build
~~~

make release 交叉编译六个平台目标到 dist，Release 包含两个二进制、README
和 rules。
