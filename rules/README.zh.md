# Agent Rules 配置说明

本目录会随每个 Release 压缩包分发。请先配置本地 MCP server，再将对应的规则文件复制到
被追踪项目中。

## 责任边界

**你只需首次手动完成：** 下载 Release、添加 MCP server、复制一个 Rules 文件，并在每个
新项目中执行一次 `ai-prov init`。

**Agent 每个任务自动执行：** 编辑前创建 session，编辑后完成 session；复制的 Rules 文件
会强制该流程。

**ai-prov 自动完成：** 创建 snapshot、计算 Diff、保存本地 provenance。你不需要手工提供
snapshot、Diff 或行数。

## 1. 使用 Release 二进制

Release 二进制带平台后缀，请优先填写其绝对路径，例如：

~~~text
/解压目录/ai-prov-darwin-arm64
/解压目录/ai-prov-mcp-darwin-arm64
~~~

在每个被追踪项目根目录执行一次初始化：

~~~sh
/解压目录/ai-prov-darwin-arm64 init
~~~

MCP server 名称固定为 `ai-prov`，command 为 `ai-prov-mcp` 二进制；它使用 stdio，
不需要参数、URL、API Key 或环境变量。不要在终端中手工运行该二进制。

## 2. 配置对应 Agent

### Codex

在仓库 `.codex/config.toml` 添加以下内容，或执行后面的命令：

~~~toml
[mcp_servers.ai-prov]
command = "/解压目录/ai-prov-mcp-darwin-arm64"
~~~

~~~sh
codex mcp add ai-prov -- /解压目录/ai-prov-mcp-darwin-arm64
~~~

将本目录的 `AGENTS.md` 复制到被追踪项目根目录，并命名为 `AGENTS.md`。

### Claude Code

在被追踪项目根目录创建或合并 `.mcp.json`：

~~~json
{
  "mcpServers": {
    "ai-prov": {
      "command": "/解压目录/ai-prov-mcp-darwin-arm64"
    }
  }
}
~~~

也可以执行
`claude mcp add --transport stdio ai-prov -- /解压目录/ai-prov-mcp-darwin-arm64`。
将 `CLAUDE.md` 复制到项目根目录。

### Cursor

在被追踪项目中创建或合并 `.cursor/mcp.json`：

~~~json
{
  "mcpServers": {
    "ai-prov": {
      "command": "/解压目录/ai-prov-mcp-darwin-arm64"
    }
  }
}
~~~

在 Cursor 的 MCP/Tools 设置中启用 `ai-prov`，再将 `cursor-rules.mdc` 复制为
`.cursor/rules/ai-prov.mdc`。

### 其他 Agent

使用同一个 command 配置名为 `ai-prov` 的 stdio MCP server，并将
`AGENT-RULES.md` 复制到该 Agent 自动加载的指令文件位置。

## 3. 验证

重启 Agent 或新开会话，应能看到 `provenance.session_start`、
`provenance.session_finish`、`provenance.verify` 三个工具。
出现 `PROJECT_NOT_INITIALIZED` 时，表示尚未在被追踪项目运行 CLI 的 `init` 命令。
