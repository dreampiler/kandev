# [→운영 세션] 승인 필요 — 업스트림 피드백 3건 작업 등록 완료(보고) + #4018 모델-확정 판단 결과 예고

## 한 줄 요약
09-29 소유자 결정(업스트림 피드백 3건)을 관제에서 접수해 등록·기동했다. 두 건은 자동 진행, 한 건(#4018)은 upstream Organizations 모델이 「굳었는지」 자체 판단 후 PR 개봉으로 이어진다. 모델이 흐리면 멈추고 이 세션/소유자에 되묻는다.

## 작업 id·순서(관제 계획 기준)
| 순서 | 작업 id | 제목 | 대상 | 현재 칸 | 승인-범위 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| 1 | 50711a7e | Upstream PR: Office dynamic profile (#8) | kdlbs#4020 | 계획(자동-기동, SCHEDULING 실측) | 기존 계획-진행 + upstream PR 개봉(본문에 Fixes #4020) |
| 2 | c6b2c860 | Upstream PR: #4022 docstring + Windows tzdata test | kdlbs#4022 | 대기(슬롯 순서로 기동) | 같은 브랜치 kd/up-office-tzdata-routines-v2 일반 push + PR 댓글 1건 |
| 3 | a6a4ce53 | Upstream PR: org-scoped dashboard (#4018) | kdlbs#4018 | 대기(슬롯 순서로 기동) | 신규 upstream PR 1건(Fixes #4018) |
| 4 | c2406b88 | Fork: adopt kdlbs#4019 maintainer fixes | fork main | 대기 | 포크 PR 1건(6bd995829 채택 + 46dd9395e 선별) — upstream 쓰기 없음 |

## 소유자 질문에 대한 관제 답(참고)
- 「#4022 Docstring Coverage 는 어떻게 할거야?」 — CodeRabbit 리뷰-원문을 PR 에서 직접 다시 읽어 건드린 함수 목록을 확정한 뒤(전달 수치 53.85%·13개만 믿지 않음) 그 함수들에 docstring 을 달아 80% 이상으로 만든다. 문체은 저장소 기존 Go 관례를 따른다.
- 「Windows 회귀 시험 같이 처리」 — 반영: 시스템 tzdata 가 없는 Windows 에서 내장 tzdata 로 시간대가 풀리는지 보는 회귀 시험을 같은 작업에서 추가한다. 둘 다 문서·시험 성격이라 하나의 push 로 PR #4022 를 갱신한다.
- kdlbs#4019 — 메인테이너가 직접 커밋을 올리는 중이므로 접근 금지를 작업 지시에 명시했다(소유자 판단 대기).

## 운영 세션이 이미 올린 답글(근거)
- kdlbs#4018: https://github.com/kdlbs/kandev/issues/4018#issuecomment-5873266862
- kdlbs#4020: https://github.com/kdlbs/kandev/issues/4020#issuecomment-5873267354
- kdlbs#4019 감사-답글(운영): https://github.com/kdlbs/kandev/pull/4019#issuecomment-5873696955

## 09-29 소유자 추가 결정(4번째 작업)
- 「감사답글 올리고 포크 반영 작업 등록 해줘」 → 작업 c2406b88 「Fork: adopt kdlbs#4019 maintainer fixes」 등록(00:5x). 메인테이너 커밋 6bd995829(+46dd9395e 선별)를 포크 main 에 반영, 포크 PR 병합 + 시험 통과. upstream PR #4019 의 head 브랜치(kd/up-codex-usage-limit)는 push 금지로 보호. 운영 설치본 반영은 소유자 결정 — 반영 직전 관제가 알림.

## 현재 제약·다음 보고
- 자원 규칙 유지: 메모리 시작 ≤88%(90% 초과 신규 금지)·한 번에 하나·GOFLAGS=-p=2·변경 패키지만.
- ko 로캘(ecd5b8c1)은 이번 승인과 무관하게 「운영판 교체 뒤」 조건 유지.
- 각 작업이 PR/댓글을 만들면 이 파일에 링크를 추가 갱신하고 관제 턴 보고에도 반영한다. 예외 외 upstream 쓰기는 여전히 소유자 결정.

(작성: Kandev 관제 8e3eb6b7, 2026-09-29 00:4x KST)
