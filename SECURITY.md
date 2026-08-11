# Security Policy

## Supported versions

YunmengZe is currently in alpha development. Security fixes are applied to the latest commit on the default branch; older snapshots and unofficial builds are not supported.

## Reporting a vulnerability

Please do **not** disclose suspected vulnerabilities, leaked credentials, bypasses, or exploit details in a public issue or discussion.

After the repository is published on GitHub, use the repository's private vulnerability reporting form under **Security → Advisories → Report a vulnerability**. Include:

- the affected version or commit;
- the operating system and runtime mode;
- a minimal reproduction or proof of concept;
- the expected and observed security boundary;
- potential impact;
- any suggested mitigation.

Do not include active third-party API keys or unrelated personal data. Revoke any credential that may have been exposed before sending the report.

## Security-sensitive areas

Reports involving the following areas are especially useful:

- policy, approval, or capability-grant bypasses;
- filesystem traversal, symlink, or junction escapes;
- command execution outside the Tool Broker (including MCP tools);
- local gateway authentication or endpoint discovery;
- secret resolution, log redaction, or configuration disclosure;
- task/run isolation, recovery, and duplicate tool execution;
- scheduled job create/fire that bypasses chat grants or the Tool Broker;
- SQLite integrity, audit tampering, or unauthorized state changes;
- denial of service caused by unbounded provider or tool behavior.

## Operator responsibilities

YunmengZe can execute tools and communicate with external model providers. Operators should:

- run it with the least operating-system privilege required;
- restrict filesystem access via `chat.workspace` / `chat.roots` (default session root = launch directory; avoid `allow_all` unless you accept full local-user path reach);
- verify policy, path ceiling, and capability grants; when `chat.permission.mode=ask`, decide tool-call permissions via TUI `/perm once|similar|permanent|deny` (permanent writes ConfigDir trust; not whole-plan approval);
- store secrets in environment variables or protected files;
- keep local configuration, databases, logs, runtime endpoints, and artifacts out of source control;
- rotate credentials immediately if accidental disclosure is suspected.

## Disclosure process

The maintainers will acknowledge a complete report, investigate it, prepare a fix where appropriate, and coordinate disclosure after affected users have a reasonable opportunity to update. Response times are best-effort while the project remains in alpha.
