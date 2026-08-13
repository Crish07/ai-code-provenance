# Agent Rules 配置说明

本目录随 Release 分发，但只是模板来源，不会被 Agent Host 自动加载。

## 首次配置

1. 使用 Release 二进制执行 `ai-prov install`。
2. 每个被追踪项目执行一次 `ai-prov init`。
3. 将 `ai-prov-mcp` 配置为 stdio MCP server，并将对应 Rules 文件复制到 Host **实际自动加载**的位置。
4. 重启 Host 并验证下列工具。

MCP command 是安装后的 `ai-prov-mcp` 绝对路径；不需要 URL、API Key 或源码上传配置。仅当 Host 无法提供 workspace root 时，才在项目级配置中设置 `AI_PROV_PROJECT_ROOT`。

## Host 示例

### Codex

在项目 `.codex/config.toml` 添加：

```toml
[mcp_servers.ai-prov]
command = "/绝对路径/ai-prov-mcp"
```

将 `AGENTS.md` 复制到被追踪项目根目录。

### Claude Code

在项目 `.mcp.json` 合并：

```json
{"mcpServers":{"ai-prov":{"command":"/绝对路径/ai-prov-mcp"}}}
```

将 `CLAUDE.md` 放入 Claude 实际加载的指令位置。

### Cursor 与其他 Host

在 Host 的 MCP 设置中配置相同 stdio command，再将 `AGENT-RULES.md` 或专用模板复制到已验证会自动加载的 Rules 位置。目录名称相似并不代表会被加载。

## 验证

Host 必须展示全部八个工具：

`provenance.session_start`、`provenance.session_heartbeat`、`provenance.session_recover`、`provenance.session_finish`、`provenance.session_abandon`、`provenance.session_status`、`provenance.verify`、`provenance.support`。

Agent 必须持久化 `session_id` 和 `agent_instance_id`、对长任务 heartbeat，并携带两个 ID finish。上下文压缩后按实例 ID recover，禁止猜测 session。失败处理见复制后的 Rules 模板。

查看 CLI 命令请使用 `ai-prov --help` 与 `ai-prov <命令> --help`。
