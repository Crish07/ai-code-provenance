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
ai-prov debug bundle --output ai-prov-debug.zip
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

## 安装与反馈

从 [Releases](https://github.com/Crish07/ai-code-provenance/releases) 下载与操作系统和 CPU
架构匹配的压缩包。先用 `SHA256SUMS.txt` 校验，再解压并将解压目录加入 `PATH`。
Windows 请使用 `.exe` 二进制；若无法配置 `PATH`，请在 MCP 客户端中填写
`ai-prov-mcp.exe` 的完整路径。

离线或企业内网发布时，请将已校验的 Release 压缩包和对应的
`SHA256SUMS.txt` 原样镜像到批准的制品仓库。运行时不需要服务端、网络连接、
源码上传或项目数据上传。

反馈问题时，请提供 `ai-prov version` 输出、失败命令、stderr 和操作系统/CPU
架构。不要附带源码、`.ai-provenance/snapshots`、Diff、token、凭据或 SQLite
数据库；请使用 Issue 模板提交脱敏信息。

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
