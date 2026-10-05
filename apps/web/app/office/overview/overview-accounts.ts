import type {
  AgentProfileUsage,
  AgentProfileUsageWindow,
} from "@/lib/api/domains/agent-profile-usage-api";
import type {
  OverviewBlockedCircuit,
  OverviewModel,
} from "@/lib/state/slices/office/overview-types";
import { isCurrentBlock } from "./overview-format";

/**
 * Provider accounts for the models section. One account is one credential a set
 * of models runs on, so its windows, its blocked state and its reset times are
 * properties of the account rather than of any single model.
 */

/** One provider account's model cards, with everything read about it. */
export type OverviewAccountGroup = {
  /** The account identity the backend derived. Empty means not identifiable. */
  accountId: string;
  /** The provider family, which the client translates into a label. */
  kind: string;
  models: OverviewModel[];
  usage: OverviewAccountUsage;
  block: OverviewAccountBlock;
};

/** What is known about one account's usage. Absent values are unknown, not zero. */
export type OverviewAccountUsage = {
  /** Every window any model of the account reported, deduplicated. */
  windows: AgentProfileUsageWindow[];
  /** True when no model of the account reported any usage at all yet. */
  pending: boolean;
  /** A bounded failure reason from a read that did not answer. */
  unavailableReason?: string;
  /** True when the reading came from what a running agent reported about itself. */
  observed: boolean;
  /** True when the reading is a last-known value rather than a live one. */
  stale: boolean;
  /** Kandev's own recorded account usage over trailing windows. */
  internal?: { label: string; turns: number }[];
  /** How many limit hits were recorded for the account. */
  limitHitCount?: number;
};

/** Whether an account is blocked right now, and until when. */
export type OverviewAccountBlock = {
  state: "clear" | "account" | "models";
  /** The earliest instant the block is expected to clear. */
  until?: string;
  /** What is blocked, named for a reader: window labels and model titles. */
  subjects: string[];
};

const ACCOUNT_KIND_KEYS: Record<string, string> = {
  opencode_zen: "overviewAccountOpencodeZen",
  opencode_go: "overviewAccountOpencodeGo",
  opencode_go_free: "overviewAccountOpencodeGoFree",
  openrouter: "overviewAccountOpenrouter",
  llmgateway: "overviewAccountLlmGateway",
  openai: "overviewAccountOpenai",
  anthropic: "overviewAccountAnthropic",
  anthropic_profile: "overviewAccountAnthropicProfile",
  google_antigravity: "overviewAccountAntigravity",
  proxy: "overviewAccountProxy",
  unknown: "overviewAccountUnknown",
};

/** The i18n key naming an account kind. An unrecognized kind reads as unknown. */
export function accountKindKey(kind: string | undefined): string {
  return ACCOUNT_KIND_KEYS[kind ?? ""] ?? ACCOUNT_KIND_KEYS.unknown;
}

/**
 * The kind an account id already names, used before the usage list answers so a
 * group keeps its label instead of reading as unknown and then renaming itself.
 * An id the backend minted that this table does not know stays unknown.
 */
export function accountKindOf(accountId: string, reported?: string): string {
  if (reported) return reported;
  if (accountId.startsWith("anthropic-profile:") || accountId.startsWith("anthropic-secret:")) {
    return "anthropic_profile";
  }
  const direct: Record<string, string> = {
    "opencode-zen": "opencode_zen",
    "opencode-go": "opencode_go",
    "opencode-go-free": "opencode_go_free",
    openrouter: "openrouter",
    llmgateway: "llmgateway",
    openai: "openai",
    anthropic: "anthropic",
    "google-antigravity": "google_antigravity",
    proxy: "proxy",
  };
  return direct[accountId] ?? "unknown";
}

/**
 * Groups concrete models by the provider account they run on. Dynamic profiles
 * are routing documents rather than accounts and are left out, so the caller
 * keeps showing them on their own. A model whose account is not identifiable
 * lands in one unnamed group rather than being dropped or given a made-up
 * account.
 *
 * A usage row without an account id is still grouped, because the same reading
 * carries the account; this only fills in what the model row itself lacks.
 */
export function groupModelsByAccount(
  models: OverviewModel[],
  byProfile: ReadonlyMap<string, AgentProfileUsage>,
  circuits: OverviewBlockedCircuit[] = [],
): OverviewAccountGroup[] {
  const blockingByProfile = blockingCircuitsByProfile(circuits);
  const groups = new Map<string, OverviewAccountGroup>();
  for (const model of models) {
    if (model.kind === "dynamic") continue;
    const usage = byProfile.get(model.agent_profile_id);
    const accountId = model.account_id ?? usage?.account_id ?? "";
    const group = groups.get(accountId) ?? {
      accountId,
      kind: accountKindOf(accountId, usage?.account_kind),
      models: [],
      usage: emptyAccountUsage(),
      block: { state: "clear", subjects: [] } as OverviewAccountBlock,
    };
    group.models.push(model);
    groups.set(accountId, group);
  }
  return [...groups.values()]
    .map((group) => ({
      ...group,
      usage: accountUsage(group.models, byProfile),
      block: accountBlock(group.models, byProfile, blockingByProfile),
    }))
    .sort(compareGroups);
}

/** Blocked accounts first, then the identified ones by label, unknown last. */
function compareGroups(a: OverviewAccountGroup, b: OverviewAccountGroup): number {
  const rank = (group: OverviewAccountGroup) => {
    if (group.block.state !== "clear") return 0;
    return group.accountId ? 1 : 2;
  };
  return (
    rank(a) - rank(b) || a.kind.localeCompare(b.kind) || a.accountId.localeCompare(b.accountId)
  );
}

/**
 * The current circuits that name a profile, indexed by that profile. A circuit
 * whose key names no profile stays out: it cannot be charged to an account it
 * may not belong to, and the blocked list still shows it.
 */
function blockingCircuitsByProfile(
  circuits: OverviewBlockedCircuit[],
): Map<string, OverviewBlockedCircuit[]> {
  const byProfile = new Map<string, OverviewBlockedCircuit[]>();
  for (const circuit of circuits) {
    if (!isCurrentBlock(circuit) || !circuit.profile_id) continue;
    const rows = byProfile.get(circuit.profile_id) ?? [];
    rows.push(circuit);
    byProfile.set(circuit.profile_id, rows);
  }
  return byProfile;
}

function emptyAccountUsage(): OverviewAccountUsage {
  return { windows: [], pending: true, observed: false, stale: false };
}

/**
 * The account's usage is the union of what its models reported. The backend
 * filters windows per model, so the union is what limits the account as a whole.
 * A model whose read failed contributes its reason rather than a zero, so an
 * unreadable account never reads as an idle one.
 */
function accountUsage(
  models: OverviewModel[],
  byProfile: ReadonlyMap<string, AgentProfileUsage>,
): OverviewAccountUsage {
  const usage: OverviewAccountUsage = { windows: [], pending: true, observed: false, stale: false };
  const seen = new Set<string>();
  let reported = false;
  for (const model of models) {
    const row = byProfile.get(model.agent_profile_id);
    if (!row) continue;
    reported = true;
    if (row.state !== "ok") {
      usage.unavailableReason ??= row.reason ?? row.state;
      continue;
    }
    usage.observed ||= row.observed === true;
    usage.stale ||= row.stale === true;
    collectWindows(usage.windows, seen, row.windows);
    usage.internal ??= internalWindows(row);
    usage.limitHitCount = accountLimitHits(usage.limitHitCount, row.limit_hits?.count);
  }
  usage.pending = !reported;
  return usage;
}

/** Adds the windows a reading reported that the union does not already hold. */
function collectWindows(
  into: AgentProfileUsageWindow[],
  seen: Set<string>,
  windows: AgentProfileUsageWindow[] | undefined,
): void {
  for (const window of windows ?? []) {
    const key = `${window.label}:${window.scope ?? ""}:${window.reset_at ?? ""}`;
    if (seen.has(key)) continue;
    seen.add(key);
    into.push(window);
  }
}

/** Kandev's own recorded usage for the account, which every reading carries. */
function internalWindows(row: AgentProfileUsage) {
  return (row.internal?.windows ?? []).map((entry) => ({ label: entry.label, turns: entry.turns }));
}

/**
 * Recorded limit hits are an account total the backend copies onto every profile
 * of that account, so adding the models' rows would count one hit once per model.
 * One account's value is used instead.
 */
function accountLimitHits(
  current: number | undefined,
  reported: number | undefined,
): number | undefined {
  if (reported === undefined) return current;
  return Math.max(current ?? 0, reported);
}

/** A whole-credential block reads as the account; named subjects read as models. */
function blockState(accountWide: boolean, hasSubjects: boolean): OverviewAccountBlock["state"] {
  if (accountWide) return "account";
  return hasSubjects ? "models" : "clear";
}

/**
 * Whether an instant is the earlier of two. A circuit's clear instant is UTC
 * while a provider window keeps its own offset, so the two are compared as
 * instants: a lexical comparison would read "20:00Z" as earlier than
 * "09:00+09:00" when the offset makes it later. An instant that cannot be parsed
 * never displaces a known one.
 */
function earlierInstant(candidate?: string, current?: string): boolean {
  if (!candidate) return false;
  if (!current) return true;
  const candidateMs = Date.parse(candidate);
  const currentMs = Date.parse(current);
  if (Number.isNaN(candidateMs)) return false;
  if (Number.isNaN(currentMs)) return true;
  return candidateMs < currentMs;
}

/**
 * Whether the account is blocked now, and what is blocked. A circuit covering a
 * whole credential, or a provider window reported exhausted without naming a
 * model, blocks the account; a circuit or window that names one model blocks
 * that model within the account.
 */
function accountBlock(
  models: OverviewModel[],
  byProfile: ReadonlyMap<string, AgentProfileUsage>,
  blockingByProfile: Map<string, OverviewBlockedCircuit[]>,
): OverviewAccountBlock {
  const subjects: string[] = [];
  let accountWide = false;
  let until: string | undefined;
  const noteUntil = (value?: string) => {
    if (earlierInstant(value, until)) until = value;
  };
  for (const model of models) {
    for (const circuit of blockingByProfile.get(model.agent_profile_id) ?? []) {
      accountWide ||= circuit.scope === "credential";
      subjects.push(circuit.model_name || circuit.profile_name || circuit.scope_value);
      noteUntil(circuit.until);
    }
    for (const window of byProfile.get(model.agent_profile_id)?.windows ?? []) {
      if (!window.limit_reached) continue;
      subjects.push(window.label);
      noteUntil(window.reset_at);
    }
  }
  const named = [...new Set(subjects.filter(Boolean))];
  const state = blockState(accountWide, named.length > 0);
  return { state, until, subjects: named };
}
