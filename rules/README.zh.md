# Agent Rules 配置说明

本目录会随每个 Release 压缩包分发，但它只是 Rules 模板来源，不是必然会被 Agent 自动加载的
目录。请先配置本地 MCP server，再让实际开发 Agent 识别其自动加载 Rules 的目录，并将对应规则
文件复制到那里。

## 责任边界

**你只需首次手动完成：** 下载 Release、添加 MCP server、让 Agent 将一个 Rules 文件安装到它
实际自动加载的位置，并在每个新项目中执行一次 `ai-prov init`。

**Agent 每个任务自动执行：** 编辑前创建 session，编辑后完成 session；前提是 Host 已确认
自动加载了该 Rules 文件。

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
不需要参数、URL、API Key 或项目路径。不要在终端中手工运行该二进制。

## 2. 配置对应 Agent

进入具体 Agent 配置前，可直接将以下内容发送给你的开发 Agent：

~~~text
请阅读 <release 解压目录>/rules/README.zh.md。识别你所在 Agent 在当前 workspace 中会自动
加载的 Rules 目录；将对应 ai-prov Rules 文件复制到该目录；配置 ai-prov MCP；并展示 Host 的
已加载 Rules 列表，证明规则已生效。不要将 <release 解压目录>/rules/ 视为自动加载目录。
~~~

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

### Trae

在项目根目录创建或合并 `.trae/mcp.json`：

~~~json
{
  "mcpServers": {
    "ai-prov": {
      "command": "/解压目录/ai-prov-mcp-darwin-arm64",
      "env": {
        "AI_PROV_PROJECT_ROOT": "${workspaceFolder}"
      }
    }
  }
}
~~~

然后完成以下操作：

1. 在项目根目录运行对应的 `ai-prov ... init`。
2. 让 Trae Agent 根据它展示的 workspace 已加载 Rules 列表，识别实际目录后，再将
   `AGENT-RULES.md` 复制到该目录。文件位于 `.trae/rules/` 不等于 Trae 已自动注入它。
3. 重启 Trae 或新开 Agent 会话，同时确认已加载 Rules 列表和三个 `provenance.*` 工具。

`AI_PROV_PROJECT_ROOT` 必须保留为 `${workspaceFolder}`，不要替换为固定项目路径。

### Qoder

在 Qoder 中打开 **Qoder Settings → MCP → My Servers → + Add**，在出现的 JSON
配置中添加或合并：

~~~json
{
  "mcpServers": {
    "ai-prov": {
      "command": "/解压目录/ai-prov-mcp-darwin-arm64"
    }
  }
}
~~~

保存后，确认 `ai-prov` 显示连接成功并可展开查看工具。将本目录的 `AGENTS.md`
复制到被追踪项目根目录；Qoder 会自动读取该文件中的规则。

### 其他 Agent

使用同一个 command 配置名为 `ai-prov` 的 stdio MCP server，并将
`AGENT-RULES.md` 复制到该 Agent 自动加载的指令文件位置。

## 3. 验证

重启 Agent 或新开会话，应能看到 `provenance.session_start`、
`provenance.session_finish`、`provenance.verify` 和 `provenance.support` 四个工具。
`provenance.support` 返回公开仓库和 GitHub Issue 地址，供 Agent 在可复现工具问题时向用户提供
正确的提交入口。
出现 `PROJECT_NOT_INITIALIZED` 时，表示尚未在被追踪项目运行 CLI 的 `init` 命令。
