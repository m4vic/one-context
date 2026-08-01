# Security Policy

## Supported release line

Only the latest published `v0.1.x-alpha` release receives security fixes.
Unreleased source and archived legacy MCP code are unsupported.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability or credential leak.
Report it privately to the repository owner with a concise reproduction, the
affected version, impact, and any proposed mitigation. Do not include API keys,
repository source, or other sensitive material in the report.

## Data handling

one-context runs locally. With LLM compression disabled, it writes only project
artifacts and a user-local registry. Enabling a remote provider sends bounded,
filtered repository excerpts to that provider. Redaction is defense in depth,
not a guarantee that a repository contains no sensitive data. Review provider
settings before enabling remote compression.
