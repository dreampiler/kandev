---
spec: docs/specs/office/requirements/workspace-overview.md
created: 2026-10-12
status: implemented
---

# Implementation Plan: Separate Automation Executions from Overview Task Counts

## Overview

The Office overview's default task counts include automation executions. The hidden per-run task (`automation_run`) was already excluded, but a visible automation-created task (`automation_task`) was counted as ordinary work, and the Office projects page task count had no origin filter at all. This plan removes every automation-execution origin from the default Office/overview counts and reports the automation volume as a separate, localized figure, without changing any task record.

## Scope

- In: the Office overview created-in-window, completed-in-window, open-task, workspace-breakdown, project-statistics, and failure-bucket reads; the Office projects page task count; the separate automation figure and the automation event feed.
- Out: analytics `/stats`, Kanban/sidebar task lists, task records/state/history, and every non-count read.

## Contract

- One automation-execution origin predicate, built from the task-models contract (`automation_run` and `automation_task`), is applied to the count reads only. Task-list reads keep the existing run-only predicate, so a visible automation task still lists.
- `ListOverviewAutomationTasks` and `ListOverviewAutomationCounts` select the same two-origin set, so the separate automation figure and the automation event feed always agree with what the default counts omit.
- The separate figure shares the accompanying aggregate's window and workspace scope, and ordinary tasks (including zero-cost work) stay counted.

## Work orders

| Task | Title |
| --- | --- |
| [task-01](task-01-separate-automation-counts.md) | Separate automation executions from the overview counts |
