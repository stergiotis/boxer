# Security Policy

## Supported versions

Only the current `main` branch is actively supported. There are no backport
branches; published tags do not receive fixes.

## Reporting a vulnerability

Please report suspected security issues through GitHub's private
vulnerability reporting:

  https://github.com/stergiotis/boxer/security/advisories/new

Do not file public issues, pull requests, or discussions for suspected
vulnerabilities — they expose the problem before a fix is available.

## Response timeline

Boxer is maintained by a single developer in spare time, so response is
best-effort. Expect an acknowledgement within 7 days for substantive
reports. There is no guaranteed timeline for a fix; severity and
exploitability inform priority.

## Scope

In scope:

- Code under `public/` (the library and binary surface).
- CI/CD configuration under `.github/` (workflow injection, credential
  exposure, supply-chain risks).

Out of scope:

- Vulnerabilities that require a malicious local user or pre-compromised
  host.
- Findings from automated scanners without a demonstrated impact path.
- Issues in third-party dependencies that are already tracked upstream;
  please report those to the upstream project. If boxer is affected by a
  specific call path, that part is in scope.
