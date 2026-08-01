# Installation

## Alpha status

`v0.1.0-alpha.1` is a new Go-based, non-MCP implementation. It is suitable for
evaluation and alpha testing, not a production stability promise. Read
`RELEASE_GATE.md` before publishing a release tag.

## Windows

1. Download the Windows archive for your architecture, extract it to a stable
   location such as `%LOCALAPPDATA%\Programs\one-context`, and add that folder
   to `PATH`.
2. In a new PowerShell session, confirm the binary:

```powershell
one-context version
one-context doctor
```

3. Register a repository once. This compiles initial context and starts the
   local daemon:

```powershell
one-context add F:\your-project
```

4. Enable automatic startup after confirming normal daemon operation:

```powershell
one-context startup install
one-context startup status
```

5. Install tool bridges if needed:

```powershell
one-context install all F:\your-project
```

Use `/one-context` in Claude Code or `$one-context` in Codex from the project.

## macOS and Linux

Build from source for the current alpha test path:

```bash
go build -o ~/.local/bin/one-context ./cmd/one-context
one-context version
one-context add ~/your-project
```

After verifying automatic context updates, install login startup:

```bash
one-context startup install
one-context startup status
```

Linux uses a systemd user service. macOS uses a LaunchAgent. Test those on the
native operating system before relying on automatic startup.

## Verify and remove

```bash
one-context status
one-context validate
one-context logs
one-context uninstall
```

`uninstall` removes startup integration and requests daemon shutdown. It does
not delete project repositories, `.one-context` artifacts, or the registry.

## Optional LLM compression

No LLM is required. Ollama is the local option:

```bash
one-context /llm ollama qwen3:4b
```

Remote providers are opt-in and receive bounded, filtered project evidence.
Use `one-context /llm limit <n>` to bound daily provider attempts, or disable
LLM compression for a sensitive repository with:

```bash
one-context config /path/to/project --llm off
```
