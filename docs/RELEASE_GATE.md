# Alpha Release Gate

Do not tag or publish `v0.1.0-alpha.1` until every item below has evidence on
the exact release commit.

- Hosted CI passes on Windows, Linux, and macOS, including Ubuntu race tests,
  cross-builds, GoReleaser validation, dependency review, and vulnerability
  scan.
- A clean-install smoke test creates a project, observes an edit, validates the
  generated artifacts, restarts the daemon, and uninstalls it.
- Windows Task Scheduler, Linux systemd user service, and macOS LaunchAgent
  have each been exercised on their native operating system.
- The generated archives contain checksums and the version command reports the
  release tag.
- Artifact provenance and SBOM/signing policy have been enabled and verified
  for the published release assets.
- At least one local Ollama and one opt-in remote-provider evaluation run has
  been compared against the evaluation corpus without unsupported claims or
  secret leakage.

The current repository is an alpha candidate only after these gates pass. Do
not present local tests or cross-compilation as proof of release readiness.
