---
status: active
system: platform
created: 2026-09-26
updated: 2026-09-26
owners:
  - dreampiler
---
# Korean Locale Requirements

## Overview

Kandev ships English, European Portuguese, Simplified Chinese, Traditional Chinese (Taiwan and Hong Kong), and Japanese catalogs. `ko` makes the already-localized web surface usable by Korean-speaking users while preserving English as the source locale, the JSON catalog contract, the `kandev_locale` cookie persistence, and the real-locale parity gate.

`ko` is already **registered** end to end (frontend `SUPPORTED_LOCALES`/`LOCALE_LABELS`, backend `supportedLocales`/`Normalize`/`FromRequest`, a `date-fns/locale/ko` loader, and the backend `locales/ko.json` browser-facing catalog). The remaining work is the UI catalog under `apps/web/src/locales/ko/`, which this document specifies.

## Terminology glossary

The Korean terminology below is the single source of truth for all `ko` catalogs. It is cross-referenced against English (source), Japanese (`ja`), and Simplified Chinese (`zh-cn`) so a Korean term is never a mechanical transliteration of English alone; where the Japanese or Chinese catalog chose a product-specific word, Korean follows the established Korean developer-tool convention.

### Core product nouns

| English | Japanese | Simplified Chinese | Korean (확정) |
| --- | --- | --- | --- |
| Workflow | ワークフロー | 工作流 | 워크플로 |
| Step | ステップ | 步骤 | 단계 |
| Task | タスク | 任务 | 작업 |
| Session | セッション | 会话 | 세션 |
| Profile | プロフィール | 配置 | 프로필 |
| Agent | エージェント | 智能体 | 에이전트 |
| Executor | 実行者 | 执行器 | 실행기 |
| Worktree | ワークツリー | 工作树 | 워크트리 |
| Repository | リポジトリ | 仓库 | 리포지토리 |
| Repo | リポジトリ | 仓库 | 리포 |
| Plan | 計画 | 计划 | 계획 |
| Review | レビュー | 审阅 | 리뷰 |
| Office | オフィス | 办公室 | 오피스 |
| Routine | ルーチン | 例行程序 | 루틴 |
| Run | 実行 | 运行 | 실행 |
| Branch | ブランチ | 分支 | 브랜치 |
| Pull request | Pull request | 拉取请求 | 풀 리퀘스트 |
| Merge request | マージリクエスト | 合并请求 | 머지 리퀘스트 |
| Issue | Issue | 议题 | 이슈 |
| Terminal | ターミナル | 终端 | 터미널 |
| Assignee | 担当者 | 负责人 | 담당자 |
| Model | モデル | 模型 | 모델 |
| Dashboard | ダッシュボード | 仪表盘 | 대시보드 |
| Approval | 承認 | 审批 | 승인 |
| Backlog | — | — | 백로그 |

### Common UI verbs

| English | Japanese | Simplified Chinese | Korean |
| --- | --- | --- | --- |
| Save | 保存 | 保存 | 저장 |
| Cancel | キャンセル | 取消 | 취소 |
| Delete | 削除 | 删除 | 삭제 |
| Close | 閉じる | 关闭 | 닫기 |
| Back | 戻る | 返回 | 뒤로 |
| Retry | 再試行 | 重试 | 다시 시도 |
| Create | 作成 | 创建 | 생성 |
| Settings | 設定 | 设置 | 설정 |

### Terms kept in English

The following are spelled identically across every locale and are never transliterated into Hangul. Most are handled by the tier-1 `looksLikeCopy` predicate (brand nouns, acronyms, protocol names) or by the shared `src/locales/_verbatim.json` registry, so they need no per-key declaration:

- **Brand/product names:** Kandev, GitHub, GitLab, Jira, Linear, Sentry, Azure DevOps, Docker, Sprites, VS Code, Telegram, Discord.
- **Acronyms & protocols:** MCP, ACP, PR, API, Git, CLI, SSH, YAML, URL, JSON, OAuth, SSE, HTTP.
- **Code-host & Git terms:** pull request (see above), merge request, issue, commit, branch, rebase, push, pull, merge — when the value is a Git command the user types or reads in Git output, it stays verbatim (already covered by the shared `_verbatim.json`).

## Style rules

1. **Cross-reference, never transliterate.** For each key, read `en`, `ja`, and `zh-cn` together and settle the meaning before translating. Match the Korean developer-tool convention, not a literal English rendering.
2. **Length by slot.** Buttons, tabs, badges, menus, table headers, and status chips use short noun or verb forms (저장, 삭제, 다시 시도), roughly within the English/Japanese length. Descriptions, help text, and confirmation sentences use natural Korean sentences.
3. **Uniform register.** Guidance and confirmation sentences use one consistent polite form (`~합니다`/`~하세요` as appropriate). Error messages follow the order **what failed + what to do**.
4. **Preserve format.** Keep `{{interpolation}}`, `<n>`/`<0>` Trans tags (matching `check-trans-indices`), shortcuts, and units exactly. Korean has no plural distinction: put the same sentence in `_one` and `_other`, with the count rendered naturally (`{{count}}개`, `{{count}}명`).
5. **No em dash (U+2014)** in any value.

## Requirements

### REQ-PLATFORM-KOREAN-LOCALE-001: Korean locale

**Intent:** Kandev ships a complete Korean UI catalog so the already-localized web surface is usable by Korean-speaking users, using the glossary and style rules above.

#### Acceptance criteria

- **AC-PLATFORM-KOREAN-LOCALE-001.1:** The shipped human locales SHALL include `ko` (Korean) alongside `en`, `pt-pt`, `zh-cn`, `zh-tw`, `zh-hk`, and `ja`.
- **AC-PLATFORM-KOREAN-LOCALE-001.2:** The Settings language switcher SHALL list Korean by its fixed endonym `한국어`. Selecting it re-renders the UI without a full reload, persists via the existing `kandev_locale` cookie, and sets `<html lang>` to `ko`.
- **AC-PLATFORM-KOREAN-LOCALE-001.3:** Frontend catalogs SHALL live at `apps/web/src/locales/ko/*.json`, with the same namespaces and key set as `en`. Each key SHALL preserve `{{placeholders}}`, plural `_one`/`_other` suffixes, `<Trans>` tag structure, code tokens, shortcuts, and brand names.
- **AC-PLATFORM-KOREAN-LOCALE-001.4:** The backend browser-facing catalog (`apps/backend/internal/i18n/locales/ko.json`) SHALL stay in sync with the glossary and the `en` backend catalog (already committed; reviewed here).
- **AC-PLATFORM-KOREAN-LOCALE-001.5:** Locale-aware date/time/number/relative-time formatting SHALL use Korean locale data (relative time reads `3주 전`, `5분 전`), driven by the active locale and the `date-fns/locale/ko` loader.
- **AC-PLATFORM-KOREAN-LOCALE-001.6:** CI, lint, and catalog-parity checks SHALL discover `ko` automatically as a real locale. A build that adds an `en` key without the `ko` translation SHALL fail, exactly as for the other real locales. The identical-to-English tolerance SHALL use the shared `_verbatim.json` registry plus a `src/locales/ko/_verbatim.json` only if a genuine value reads the same in Korean.
- **AC-PLATFORM-KOREAN-LOCALE-001.7:** `pnpm run i18n:check`, `pnpm run typecheck`, and `pnpm run lint` SHALL pass from `apps/web` with zero missing keys, zero index mismatches, and zero identical-to-English prose.
- **AC-PLATFORM-KOREAN-LOCALE-001.8:** The Playwright `language-switch.spec.ts` SHALL cover selecting `한국어`, verifying `lang="ko"` and the known translated label `표시 언어`, cookie persistence (`kandev_locale=ko`), reload restoration, and restoring English.

## Explicit exclusions

- Translating the CLI launcher, logs, ACP/agent output, or diagnostic API error strings (these stay English by design).
- Translating user/domain content carried in the boot payload and store (task titles, workflow/step names, repo names, chat messages, diff content, agent transcripts).
- RTL layout support; `<html dir>` stays `ltr`.
- Translating integration provider names, brand names, code identifiers, keyboard-shortcut glyphs, and other proper nouns (see "Terms kept in English").
- A translation-management platform / vendor sync.
