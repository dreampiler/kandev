---
id: "02-resume-verification"
title: "Resume verification"
status: pending
wave: 2
depends_on:
  - "01-notification-compatibility"
plan: "plan.md"
requirements:
  - REQ-AGENTS-MCP-PROTOCOL-001
acceptance_criteria:
  - AC-AGENTS-MCP-PROTOCOL-001.9
system_design:
  - ../../specs/agents/system-design/mcp-protocol-compatibility.md
---

# Task 02: Resume Verification

## Scope and acceptance

In an isolated runtime, use Gemini 3.8 Flash with Antigravity ACP 1.2.1 to invoke
a harmless Kandev read tool. Allow idle, restore the same native conversation,
then invoke that tool again. Confirm the actual tool result and absence of the
reported MCP initialization error. Do not substitute a new session or fallback
model. Keep existing credentials and the operating runtime unchanged.

If isolated authenticated real-client verification cannot be performed, record
the missing prerequisite and hand the exact sequence to the release's
post-deployment verification owner. Do not claim synthetic HTTP evidence proves
real-client recovery.

## Verification and delivery

Use existing isolated-runtime and ACP facilities; do not add permanent tests or
new authentication/configuration. Record bounded resume/tool evidence internally,
without publishing secrets, personal paths, or session identifiers. Stop only
processes started for this check, after verifying their PIDs and command lines.

After local verification, publish the correction for review. Deployment and the
final operating-session check remain with the release owner.

## Results

Antigravity ACP 1.2.1 initialization, session creation, Gemini 3.8 Flash
selection, and an initial harmless MCP fixture call were observed. Full resume
verification remains pending: the replacement process's initialization exceeded
the overall 240-second probe budget before `session/load` could be sent.

Earlier temporary probe attempts timed out or rejected the fixture tool's ACP
permission request through the debug runner's automatic method-not-found reply.
A temporary overlay corrected that debug-runner limitation without modifying
the repository's runner. These probe failures are not evidence of a newly
diagnosed production defect. No operational instance was changed.

The release verification owner must still perform the exact idle, same-native-
conversation restore, and repeated Kandev read-tool sequence. Do not mark this
work order done based on initial-call or wire-only evidence.
