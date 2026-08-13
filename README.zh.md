# ai-code-provenance

面向 MCP Coding Agent 的本地 AI 代码来源追踪工具。ai-prov 在本地记录 AI session、计算真实工作区变更，并为 Git 新增行计算**AI 来源覆盖率**；不会上传源码、Diff 或项目文件。

[English README](README.md)

## 安装与初始化

下载对应 Release 并校验 `SHA256SUMS.txt` 后，解压并进入解压目录，使用其中的 `ai-prov` 执行：

```sh
# macOS / Linux
./ai-prov install
# 切换到工作目录新开终端后：
ai-prov init
```

```powershell
# Windows PowerShell
.\ai-prov.exe install
# 切换到工作目录新开终端后：
ai-prov init
```

`install` 仅为当前用户复制 `ai-prov`、`ai-prov-mcp` 并添加 ai-prov 自有 PATH 项；`uninstall` 仅删除安装收据中、hash 仍匹配的文件。它绝不会删除项目 `.ai-provenance`、MCP 配置、Rules 或 Git hook。PATH 变更后请新开终端；安装完成后可用 `ai-prov install --dry-run` 或 `ai-prov uninstall --dry-run` 预览对应操作。

每个被追踪项目只需执行一次：

```sh
ai-prov init
```

项目状态存于 `.ai-provenance`，请在被追踪项目中忽略该目录。

## MCP 与 Rules

将 `ai-prov-mcp` 配置为本地 stdio MCP server，再将 Release 中的一份 Rules 模板复制到 Agent **实际自动加载**的位置。Release 的 `rules/` 只是模板来源，不代表 Host 会自动加载。

详见 [Rules 配置说明](rules/README.zh.md)。

## Agent 每个任务的流程

1. 为 Agent 实例生成并持久化一个 UUID `agent_instance_id`。
2. 编辑前调用 `provenance.session_start`，持久化返回的 `session_id` 和 `agent_instance_id`。
3. 长任务期间使用两个 ID 调用 `provenance.session_heartbeat`。
4. 完成时用两个 ID 调用 `provenance.session_finish`，必须得到 `finished`。
5. 提交前可运行 `ai-prov verify --scope staged --strict`。

上下文压缩丢失 session ID 后，使用已保存的实例 ID 调用 `provenance.session_recover`，不得猜测候选。超过 heartbeat 租约的 session 会变为 `failed / SESSION_LEASE_EXPIRED`，应新建 session，不能 finish。

AI 来源覆盖率只表示 staged/worktree 新增有效行中匹配已完成 AI provenance 的比例；它不表示 token、费用、对话轮数、耗时，也不区分人机混编。

## CLI 完整命令参考

除 `install`、`uninstall`、`version`、`completion` 外，其余项目级命令都必须在已执行 `ai-prov init` 的项目根目录运行。所有命令均可追加 `--help` 查看当前安装版本的准确参数。

### 初始化、状态与版本

| 命令                                       | 用途与备注                                                                                              |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| `ai-prov init`                             | 初始化当前项目的 `.ai-provenance/`、默认配置、SQLite 数据库及 snapshot 目录。可重复执行；不会上传代码。 |
| `ai-prov status`                           | 输出项目绝对路径，以及 `active`、`finished`、`failed` 三种 session 的计数。适合先确认项目是否可用。     |
| `ai-prov version`                          | 输出当前 CLI 的版本、commit 和构建时间；排查“Rules、MCP 与二进制是否同一版本”时应首先执行。             |
| `ai-prov --help` / `ai-prov <命令> --help` | 列出命令或子命令及其 flags；这是判断本机实际可用能力的权威入口。                                        |

### Session 与 snapshot 管理

| 命令                                     | 用途与备注                                                                                                                            |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `ai-prov sessions active`                | 列出仍为 active 的 session，不读取或输出源码内容。默认每行输出 session ID、开始时间、Agent、任务、文件数和 snapshot 字节数。          |
| `ai-prov sessions active --json`         | 将 active session 列表序列化为 JSON，便于脚本读取；不包含源码内容。                                                                   |
| `ai-prov snapshots gc`                   | **默认 dry-run**：预览已超过终态 snapshot 保留期的可回收 session、对象数量及字节数；不删除任何内容。                                  |
| `ai-prov snapshots gc --older-than 168h` | 临时覆盖项目配置中的终态 snapshot 保留时长；`168h` 表示 7 天。该 flag 不修改配置文件。                                                |
| `ai-prov snapshots gc --json`            | 将 GC 预览/执行结果序列化为 JSON。                                                                                                    |
| `ai-prov snapshots gc --apply`           | 按当前候选集实际删除终态 snapshot/object，属于破坏性操作；应先运行不带 `--apply` 的命令核对范围。可与 `--older-than`、`--json` 组合。 |

lease 过期的 snapshot 仅在项目显式启用 `auto_reclaim_expired_sessions: true` 后，才会在宽限期结束时自动进入回收流程；普通 CLI GC 始终默认 dry-run。

### 覆盖率校验与 report 序列化输出

`verify` 输出汇总统计；`report` 在相同统计基础上额外逐行标注 AI/unknown 来源，并列出工作区扫描时跳过的文件。两者都只分析本地 Git diff 和本地 provenance 数据，不会上传任何内容。

| 命令                              | 用途与备注                                                                                                                                          |
| --------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ai-prov verify`                  | 校验暂存区（默认 `staged`）新增行是否已被完成的 AI session 覆盖，输出覆盖率、引用 session 与未覆盖文件。若结果非 `ok`，命令以非零退出码结束。       |
| `ai-prov verify --scope worktree` | 改为校验工作区相对 Git 的变更；`--scope` 仅接受 `staged` 或 `worktree`。                                                                            |
| `ai-prov verify --strict`         | 严格模式：只要存在未覆盖新增行即失败，适合 CI 或提交前门禁。                                                                                        |
| `ai-prov verify --json`           | 向标准输出写入与 MCP `provenance.verify` 相同字段的 JSON 汇总。                                                                                     |
| `ai-prov report`                  | 输出新增行的逐文件、逐行来源（`AI` 或 `unknown`）以及 workspace 扫描跳过项；默认 scope 为 `staged`。它是 CLI 专属能力，MCP 没有同名 `report` 工具。 |
| `ai-prov report --scope worktree` | 用工作区变更生成逐行 report。                                                                                                                       |
| `ai-prov report --json`           | 向标准输出写入完整 JSON report，包含统计、每行文本和跳过项；命令**不会自动创建报告文件**。                                                          |

将 report 保存为文件时，使用 shell 重定向：

```sh
# 暂存区报告：写入当前目录的 JSON 文件
ai-prov report --json > ai-prov-report.json

# 工作区报告：同样写入 JSON 文件
ai-prov report --scope worktree --json > ai-prov-report-worktree.json

# 只看人类可读的逐行输出，不保存文件
ai-prov report --scope staged
```

`report --json` 的字段如下；`files[].added_lines[].content` 是真实新增行文本，报告可能包含源代码，保存、上传或分享前必须自行审查。

```jsonc
{
  "status": "ok", // ok 或 warning
  "scope": "staged", // staged 或 worktree
  "total_added_lines": 5, // 全部新增有效行数
  "ai_added_lines": 5, // 已匹配 AI provenance 的新增行数
  "untracked_added_lines": 0, // 未匹配 provenance 的新增行数
  "coverage": 1, // AI 来源覆盖率，范围 0～1
  "sessions": ["<session-uuid>"], // 本次统计引用的完成 session
  "uncovered_files": [], // 含 unknown 新增行的文件路径
  "files": [
    {
      "path": "internal/example.go", // 项目相对路径
      "added_lines": [
        {
          "content": "func Example() {}", // 新增行原文：可能含源码
          "source": "AI", // AI 或 unknown
          "session_id": "<session-uuid>", // 仅 AI 行出现，指向其来源 session
        },
      ],
    },
  ],
  "skipped": [
    {
      "path": "assets/logo.png", // 未参与 workspace 扫描的路径
      "reason": "non_utf8_or_binary", // 跳过原因；例如二进制或非 UTF-8
    },
  ],
}
```

### 安装与卸载

| 命令                                                | 用途与备注                                                                                                                                           |
| --------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `./ai-prov install`（macOS/Linux Release 解压目录） | 首次安装入口；将当前 release 的 `ai-prov` 和 `ai-prov-mcp` 复制到当前用户目录，并追加 ai-prov 自有 PATH 片段。Windows 使用 `.\ai-prov.exe install`。 |
| `ai-prov install --dry-run`                         | 只校验 release 文件和目标路径，不复制文件、不修改 PATH、不写安装收据。                                                                               |
| `ai-prov install --dir /绝对路径`                   | 覆盖用户级安装目录；必须使用你拥有的绝对路径。                                                                                                       |
| `ai-prov install --no-path`                         | 安装二进制但不修改 shell profile/Windows 用户 PATH；之后需自行以绝对路径或自己的 PATH 配置调用。                                                     |
| `ai-prov install --force`                           | 仅在目标 ai-prov 管理的二进制与 release 内容不一致时允许替换；不影响项目 provenance 数据。                                                           |
| `ai-prov uninstall --dry-run`                       | 预览会移除的、收据记录且 SHA-256 仍匹配的二进制和 PATH 片段；不实际删除。                                                                            |
| `ai-prov uninstall`                                 | 仅移除安装收据拥有且 hash 未变的二进制及 ai-prov 自有 PATH 项；绝不删除 `.ai-provenance`、MCP 配置、Rules 或 Git hook。                              |
| `ai-prov uninstall --keep-path`                     | 卸载二进制但保留已记录的 ai-prov PATH 项。                                                                                                           |

### Git Hook 与 trailer

| 命令                                                              | 用途与备注                                                                                                                                                   |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `ai-prov hook install`                                            | 在当前 Git 项目安装 `commit-msg` hook，并启用 title 覆盖率模式：title 追加 `[AI:<n>%]`，末尾 trailer 写入 `lines,agent`；若已有非 ai-prov hook，会拒绝覆盖。 |
| `ai-prov hook install --force`                                    | 先备份已有 `commit-msg` hook，再安装 ai-prov hook；仅在确认可备份该 hook 时使用。                                                                            |
| `ai-prov hook install --trailer-only`                             | 安装 hook 但不改 title；末尾 trailer 默认写入 `coverage,lines,agent`。                                                                                       |
| `ai-prov hook uninstall`                                          | 仅卸载 ai-prov 自己管理的 hook；外部 hook 保留或恢复备份。                                                                                                   |
| `ai-prov hook config show`                                        | 显示当前有效 trailer 字段和注释开关，并附带中文字段解释。                                                                                                    |
| `ai-prov hook config set --fields coverage,agent --comments=true` | 设置 trailer 字段；仅显式指定时写入 `# ai-prov trailer` 注释，默认 `false`。可选字段仅为 `coverage`、`lines`、`agent`、`provenance-id`；逗号分隔、不可重复。 |
| `ai-prov hook config reset`                                       | 恢复 title 覆盖率模式和 `lines,agent` trailer，并关闭注释。                                                                                                  |

`hook config set --fields ...` 只改变末尾 trailer 字段，不改变 title 覆盖率模式。`lines` 可显示“已记录 AI 新增行/全部新增行”，`provenance-id` 显示贡献 session ID 的前 8 位。项目没有可验证的 confidence 算法，因此不会输出 `AI-Confidence`。

### 诊断与命令补全

| 命令                                                      | 用途与备注                                                                                                                       |
| --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `ai-prov debug bundle`                                    | 在当前目录创建隐私安全的诊断 zip，并输出其路径。zip 仅含运行时元数据，不含源码、Diff、数据库、snapshot、token、凭据或 Git 配置。 |
| `ai-prov debug bundle --output /绝对路径/diagnostics.zip` | 指定诊断 zip 输出路径；文件名必须以 `.zip` 结尾，且目标不得已存在。                                                              |
| `ai-prov completion bash` / `zsh` / `fish` / `powershell` | 输出对应 shell 的自动补全脚本；请按 `ai-prov completion <shell> --help` 中给出的加载方式配置。                                   |
| `ai-prov completion <shell> --no-descriptions`            | 生成不带命令说明的补全脚本，适用于希望缩小补全脚本体积的环境。                                                                   |

## 可选 Git trailer

在项目根目录运行 `ai-prov hook install` 后，默认在 title 写入已验证覆盖率，并在末尾保留简洁审计信息：

```text
feat: add thing [AI:100%]

AI-Lines: 5/5
AI-Agent: codex
```

使用 `ai-prov hook install --trailer-only` 可保持 title 不变，改为在末尾写入 `AI-Contribution`、`AI-Lines`、`AI-Agent`。`ai-prov hook config set --fields ...` 仅配置末尾 trailer 字段；注释默认关闭，只有显式 `--comments=true` 才会写入。由于没有可验证的 confidence 算法，ai-prov 不再输出 `AI-Confidence`。`hook.write_trailer: false` 时 hook 仍会清理历史 ai-prov trailer，且不会影响其他 Git trailer。

## MCP 工具

| 工具                           | 用途                                    |
| ------------------------------ | --------------------------------------- |
| `provenance.session_start`     | 创建 session 与基线。                   |
| `provenance.session_heartbeat` | 为所属实例续租。                        |
| `provenance.session_recover`   | 恢复唯一 active session，可按实例筛选。 |
| `provenance.session_finish`    | 保存本地 Diff 与行来源。                |
| `provenance.session_abandon`   | 显式终止确认不再完成的 session。        |
| `provenance.session_status`    | 不读取源码地查询状态。                  |
| `provenance.verify`            | 校验新增行覆盖率。                      |
| `provenance.support`           | 返回仓库与问题反馈地址。                |

## 开发

```sh
gofmt -w <修改的 Go 文件>
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/ai-prov ./cmd/ai-prov-mcp
```

## 许可证

[MIT](LICENSE)
