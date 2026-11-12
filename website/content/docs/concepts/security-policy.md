---
title: "Security Policy"
weight: 3
description: "How to report a vulnerability, which versions get fixes, and how disclosure works."
source: "SECURITY.md"
---

## Reporting a Vulnerability

Report vulnerabilities privately through GitHub:

https://github.com/container-registry/harbor-satellite/security/advisories/new

Do not open a public issue, pull request or discussion for a suspected vulnerability. Include the affected version or commit, the component (satellite, Ground Control, `groundcontrol` CLI, Helm chart), the configuration needed to reproduce it, and the impact you expect. A proof of concept helps but is not required.

Vulnerabilities in Harbor itself go to the Harbor project ([goharbor/harbor security policy](https://github.com/goharbor/harbor/security/policy)), and those in SPIRE, PostgreSQL or another dependency to that project. If you are unsure where a problem belongs, report it here and we forward it.

## Supported Versions

| Version | Security fixes |
|---|---|
| 1.0.x | yes |
| 0.0.x and earlier | no, upgrade to 1.0 ([upgrade notes](/docs/operations/upgrading/)) |

Fixes are released as a new patch version of the latest supported minor version. Older patch versions of that minor are not patched separately.

## What to Expect

- We acknowledge the report within 3 business days.
- We confirm or reject the issue and give a first severity assessment within 10 business days, and ask you for more information when we cannot reproduce it.
- We keep you informed in the advisory while we work on a fix, and agree a disclosure date with you.

## Disclosure Process

1. The report is handled in a private GitHub security advisory on this repository. Collaborators you name can be added to it.
2. The fix is developed in the advisory's private fork and reviewed there.
3. We request a CVE through GitHub for confirmed vulnerabilities.
4. We publish a patch release, then the advisory with the affected and fixed versions, the CVE and credit to the reporter, unless you ask not to be named.
5. We aim to publish within 90 days of the report. An issue that is exploited in the wild may be disclosed earlier, with mitigations if no fix is ready yet.

## Security Model

What each component trusts, which secrets exist and how to harden a deployment is described in the [security model](/docs/concepts/security/).
