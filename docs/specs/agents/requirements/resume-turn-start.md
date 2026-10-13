---
status: draft
system: agents
created: 2026-10-13
owners:
  - Kandev
---

# Restart Resume Turn Start Requirements

## Overview

An unclean backend stop during an open turn must not end the conversation. When
the backend returns, the affected session either resumes and genuinely begins a
new turn, or it reports an explicit failure. A session that is only waiting keeps
waiting.

This capability owns the observable outcome "a restart-interrupted session
starts a real turn". The agent resume contract, the dynamic routing contract, and
the task delivery contract each contribute a part of that outcome; this document
owns only the combined, testable result that no contributor currently owns on
its own.

## Terminology

- **Turn start:** the persisted transition that creates a new agent execution and
  emits that execution's first agent event for an existing task session.
- **Explicit failure:** a terminal, user-visible resume or launch outcome that
  names its cause, as opposed to a silent stop or an unconfirmed success.
- **Waiting session:** a session parked on a parent, owner, blocker, or paused
  task that must not run until an answer or an explicit resume.
- **Route pending:** the persisted dynamic-route state that marks a resumed
  candidate whose downstream agent session is not yet attached.

## Requirements

### REQ-AGENTS-RESUME-TURN-START-001: Restart-interrupted sessions start a real turn

**Intent:** After an unclean backend stop during an open turn, an operator must be
able to observe a genuine new turn (a new execution plus its first event) or an
explicit failure. A silent stop that only looks like success is not acceptable.

#### Acceptance criteria

- **AC-AGENTS-RESUME-TURN-START-001.1:** When the backend stops during an open turn and restarts, the affected session shall reach either a terminal resume outcome (`resumed` or `relaunched`) or an explicit failure. It shall not remain indefinitely pending.
- **AC-AGENTS-RESUME-TURN-START-001.2:** A resume reported as successful shall have linked a new execution and emitted that execution's first agent event for the same task session.
- **AC-AGENTS-RESUME-TURN-START-001.3:** When an automatic resume is refused, an explicit continuation request (continue from history) shall begin a real turn, the new instruction shall be reflected in the response, and no resume-unavailable error shall remain current.
- **AC-AGENTS-RESUME-TURN-START-001.4:** A message sent to a session whose previous turn was interrupted shall start a new turn for both a fixed profile and a dynamic profile. It shall not be accepted and then silently dropped.
- **AC-AGENTS-RESUME-TURN-START-001.5:** A waiting session (parent, owner, blocker, or paused task) shall be preserved across the restart with its wait, reference, and intent intact, shall perform zero automatic re-dispatch, and shall advance only after an answer or an explicit resume.
- **AC-AGENTS-RESUME-TURN-START-001.6:** Delivery addressed to a task whose primary session is being handed over shall be bound to the new generation and acknowledged, shall not create a duplicate turn, and shall retain the original message text.
- **AC-AGENTS-RESUME-TURN-START-001.7:** A dynamic session interrupted by the restart shall resolve to `resumed` or `relaunched` on a healthy candidate, or to an explicit failure. A route pending state shall not be terminal.

## Out of scope

- Reinterpreting the stored provider conversation, or reconstructing provider-owned history.
- Relaxing credential-broker or terminal-session authorization.
- Automatic continuation on a new branch after branch loss, which belongs to [Agent Resume and Runtime Recovery](agent-resume-runtime-recovery.md).
- New recovery controls, pages, or copy beyond the existing recovery surfaces.
- The fork release-confirmation tooling (`scripts/verify-release.ps1`). It is operational verification, not product behavior, and does not define a requirement here.
