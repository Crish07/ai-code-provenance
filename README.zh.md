# ai-code-provenance

面向 MCP Coding Agent 的本地 AI 代码来源追踪工具。

本项目在本地记录已声明的 AI 编码会话，计算真实工作区变更，并为 Git变更生成来源覆盖率；不会上传源码、Diff 或项目文件。

[English README](README.md)

## 组件

- ai-prov：初始化、状态、校验和报告 CLI。
- ai-prov-mcp：记录 session 与 provenance 的 stdio MCP server。
- rules：要求兼容 Agent 使用 MCP 的模板。

本地状态保存在 .ai-provenance，请在被追踪项目中忽略该目录。

## 快速开始

```sh
make build
cd /path/to/project
ai-prov init
```

请先完成 MCP 配置，再让实际开发 Agent 阅读 release 包中的 `rules/`，自行识别该 Agent真正会自动加载的 Rules 目录，并复制对应模板。
不要因为目录名称相似就假定 Host 会加载它。
具体配置请务必查看 [rules/README.zh.md](rules/README.zh.md)。

强制流程：编辑前调用 provenance.session_start；完成修改；
调用 provenance.session_finish 并确认 finished；
提交前可运行：

```sh
ai-prov verify --scope staged --strict
```

start 或 finish 失败时，Agent 不得宣称任务完成；未记录新增行是未覆盖行，绝不会被视为 AI 代码。

## 命令

```sh
ai-prov version
ai-prov init
ai-prov status
ai-prov debug bundle --output ai-prov-debug.zip
ai-prov verify --scope staged --strict --json
ai-prov report --scope staged --json
ai-prov hook install
ai-prov hook uninstall
```

## 在 Git 提交信息中记录 AI 结果（可选）

MCP 的 `session_finish` 只保存本地 provenance；它**不会**自动修改 Git 提交信息。
如需在每次 `git commit` 时自动记录暂存区的校验结果，请在被追踪项目根目录手动安装一次：

```sh
ai-prov hook install
```

安装后，hook 会在提交时运行 staged 校验，并将结果追加到 commit message 末尾：

```text
AI-Contribution: 100%
AI-Lines: 5/5
AI-Agent: codex
AI-Confidence: 100%
AI-Provenance-ID: abcdef12
```

它不会修改提交标题；没有新增行时不会追加空记录。执行 `ai-prov hook uninstall` 可移除该行为。
将 `.ai-provenance/config.yaml` 中 `hook.write_trailer` 设为 `false`时，hook 仅校验，不写入提交信息。

## MCP 工具

| 工具                      | 用途                               |
| ------------------------- | ---------------------------------- |
| provenance.session_start  | 创建 session 和基线 snapshot。     |
| provenance.session_finish | 计算本地 Diff 并保存 provenance。  |
| provenance.verify         | 校验 staged 或 worktree 新增行。   |
| provenance.support        | 返回公开仓库与 GitHub Issue 地址。 |

未初始化时运行 ai-prov init；基线冲突时重新创建 session；存储锁定时稍后重试。

## 配置 Rules 与 MCP

每个 release 压缩包都包含 `rules/` 目录。
请先配置本地 stdio MCP server，再让实际使用的 Agent 阅读 `rules/README.zh.md`，
识别它自己在当前工作区会自动加载的 Rules 目录，并将对应模板复制过去。
release 包中的 `rules/` 只是模板来源，不会被所有 Host 自动加载。
完整、可直接复制的配置集中在 [rules/README.zh.md](rules/README.zh.md)。
Trae 用户必须使用其中带`${workspaceFolder}` 的 MCP 配置。

### 需要你手动完成一次的操作

1. 下载并解压与你的平台匹配的 Release 压缩包。
2. 让 Agent 阅读 [rules/README.zh.md](rules/README.zh.md)，配置 `ai-prov-mcp`，
   并把对应Rules 文件复制到该 Agent 实际自动加载的位置。
3. 在每个需要追踪的项目根目录，用 release 包中的 `ai-prov` 二进制执行 `init`。

不清楚 Rules 目录时，直接把下面这段话发送给你的开发 Agent：

```text
请阅读 <release 解压目录>/rules/README.zh.md。识别你所在 Agent 在当前 workspace 中会自动注入的 Rules 目录；
把对应的 ai-prov Rules 文件复制到该目录；配置 ai-prov MCP；最后展示 Host的已加载 Rules 列表，
证明该规则已生效。不要把 <release 解压目录>/rules/ 本身当作自动加载目录。
```

以上是安装和首次配置操作。第 3 步只需在新项目中执行一次，不需要每次开发任务都重复。

### Agent 在每个编码任务中自动执行的操作

仅在 Host 已确认加载该 Rules 文件后，Rules 才会要求 Agent 在编辑前调用 `provenance.session_start`，
正常编辑代码，再调用 `provenance.session_finish` 并确认结果为 `finished`；
提交前可调用 `provenance.verify`。你不需要手工创建 snapshot、计算 Diff 或填写行数。

### ai-prov 工具自动完成的操作

本地 MCP server 会自动创建基线 snapshot、计算真实工作区 Diff、将 provenance记录到 `.ai-provenance/`，并返回 session 结果。
源码和 provenance 数据始终保留在本地。

## 安装与反馈

从 [Releases](https://github.com/Crish07/ai-code-provenance/releases) 下载与操作系统和 CPU 架构匹配的压缩包。
先用 `SHA256SUMS.txt` 校验，再解压。压缩包中的二进制带平台后缀，请直接使用完整路径，或按「在 Agent 中配置本地 MCP」
一节重命名后再加入 `PATH`。Windows 请使用 `.exe` 二进制，并在 MCP 客户端中填写其完整路径。

离线或企业内网发布时，请将已校验的 Release 压缩包和对应的`SHA256SUMS.txt` 原样镜像到批准的制品仓库。
运行时不需要服务端、网络连接、源码上传或项目数据上传。

反馈问题时，请提供 `ai-prov version` 输出、失败命令、stderr 和操作系统 / CPU 架构。
不要附带源码、`.ai-provenance/snapshots`、Diff、token、凭据或 SQLite 数据库；
请使用 Issue 模板提交脱敏信息。

## 开发

```sh
make fmt
make test
make test-race
make vet
make build
```

make release 交叉编译六个平台目标到 dist，Release 包含两个二进制、README 和 rules。
