"use client";

import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@kandev/ui/alert-dialog";
import { Button } from "@kandev/ui/button";
import { settingsActionClassName } from "@/components/settings/settings-control";
import { resolveDynamicReferencesAction } from "@/app/actions/agents";
import type { AgentProfile } from "@/lib/types/http";
import type {
  DynamicProfileReference,
  DynamicReferenceConflictLists,
} from "@/lib/types/agent-profile-errors";

export type DynamicReferenceDialogMode = "disable" | "delete";
export type DynamicReferenceChoice = "disable_candidate" | "remove_candidate";

/**
 * Maps a display choice onto the effective server operation. Disabling the
 * candidate always disables the profile (both entry points), so it never pairs
 * with `delete`; removing the candidate keeps the entry point's operation
 * (disable on the disable entry, delete on the delete entry).
 */
export function resolveEffectiveOperation(
  entry: DynamicReferenceDialogMode,
  choice: DynamicReferenceChoice,
): DynamicReferenceDialogMode {
  return choice === "disable_candidate" ? "disable" : entry;
}

type AgentProfileDynamicReferenceDialogProps = {
  profileName: string;
  mode: DynamicReferenceDialogMode;
  references: DynamicProfileReference[];
  pending: boolean;
  onCancel: () => void;
  onChoose: (choice: DynamicReferenceChoice) => void;
};

/**
 * Shared confirmation that resolves the dynamic profiles referencing a concrete
 * profile before it is disabled or deleted. It offers the same two positive
 * choices plus Cancel on desktop and phone; the least destructive choice
 * (disable the candidate) is the Enter default.
 */
export function AgentProfileDynamicReferenceDialog({
  profileName,
  mode,
  references,
  pending,
  onCancel,
  onChoose,
}: AgentProfileDynamicReferenceDialogProps) {
  const { t } = useTranslation();
  return (
    <AlertDialog
      open={references.length > 0}
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
    >
      <AlertDialogContent
        data-testid="agent-profile-dynamic-reference-dialog"
        data-layout="contained"
        className="max-h-[calc(100dvh-2rem)] max-w-[calc(100vw-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden"
      >
        <AlertDialogHeader>
          <AlertDialogTitle>
            {mode === "delete"
              ? t("agents:dynamicReferenceDeleteTitle", { name: profileName })
              : t("agents:dynamicReferenceDisableTitle", { name: profileName })}
          </AlertDialogTitle>
          <AlertDialogDescription>{t("agents:dynamicReferenceDescription")}</AlertDialogDescription>
        </AlertDialogHeader>
        <div
          data-testid="agent-profile-dynamic-reference-body"
          className="min-h-0 min-w-0 space-y-2 overflow-x-hidden overflow-y-auto overscroll-contain text-left text-sm"
        >
          <p className="font-medium">{t("agents:dynamicReferenceIntro")}</p>
          <ul className="list-disc list-inside space-y-0.5">
            {references.map((reference) => (
              <li key={reference.id} data-testid="agent-profile-dynamic-reference-row">
                <span className="font-medium">{reference.name || reference.id}:</span>{" "}
                {reference.routes
                  .map((route) =>
                    t("agents:dynamicReferenceRoute", {
                      tier: route.tier,
                      position: route.position + 1,
                    }),
                  )
                  .join(", ")}
              </li>
            ))}
          </ul>
        </div>
        <AlertDialogFooter data-testid="agent-profile-dynamic-reference-footer">
          <AlertDialogCancel className={settingsActionClassName("w-full cursor-pointer sm:w-auto")}>
            {t("common:cancel")}
          </AlertDialogCancel>
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            className={settingsActionClassName("w-full cursor-pointer sm:w-auto")}
            onClick={() => onChoose("remove_candidate")}
            data-testid="agent-profile-dynamic-reference-remove"
          >
            {mode === "delete"
              ? t("agents:dynamicReferenceRemoveThenDelete")
              : t("agents:dynamicReferenceRemoveCandidate")}
          </Button>
          <Button
            type="button"
            disabled={pending}
            data-dialog-default-action
            className={settingsActionClassName("w-full cursor-pointer sm:w-auto")}
            onClick={() => onChoose("disable_candidate")}
            data-testid="agent-profile-dynamic-reference-disable"
          >
            {mode === "delete"
              ? t("agents:dynamicReferenceDeleteDisableInstead")
              : t("agents:dynamicReferenceDisableCandidate")}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

// DynamicReferenceRequest is the pending confirmation: which profile is being
// disabled or deleted and the dynamic profiles that reference it.
export type DynamicReferenceRequest = {
  profileId: string;
  profileName: string;
  operation: DynamicReferenceDialogMode;
  references: DynamicProfileReference[];
};

type DynamicReferenceDialogHandlers = {
  onResolved: (result: {
    operation: DynamicReferenceDialogMode;
    profile: AgentProfile | null;
  }) => void;
  onBlocked: (conflict: DynamicReferenceConflictLists) => void;
  onError: (message: string) => void;
};

/**
 * Owns one pending resolution confirmation and its action call. Consumers open
 * the dialog with `request(...)`, then run their own post-resolution store
 * update from `onResolved`; a non-dynamic blocker (routing tier, session,
 * watcher, automation, utility) is handed to `onBlocked` so the existing
 * conflict surface can take over.
 */
export function useDynamicReferenceDialog(handlers: DynamicReferenceDialogHandlers) {
  const [request, setRequest] = useState<DynamicReferenceRequest | null>(null);
  const [pending, setPending] = useState(false);

  const choose = useCallback(
    async (choice: DynamicReferenceChoice) => {
      if (!request) return;
      setPending(true);
      const versions = Object.fromEntries(request.references.map((ref) => [ref.id, ref.version]));
      const operation = resolveEffectiveOperation(request.operation, choice);
      const result = await resolveDynamicReferencesAction(request.profileId, {
        operation,
        action: choice,
        versions,
      });
      setPending(false);
      if (result.status === "ok") {
        setRequest(null);
        handlers.onResolved({ operation, profile: result.profile });
        return;
      }
      if (result.status === "conflict") {
        setRequest(null);
        handlers.onBlocked(result);
        return;
      }
      handlers.onError(result.message);
    },
    [request, handlers],
  );

  const dialog = (
    <AgentProfileDynamicReferenceDialog
      profileName={request?.profileName ?? ""}
      mode={request?.operation ?? "disable"}
      references={request?.references ?? []}
      pending={pending}
      onCancel={() => setRequest(null)}
      onChoose={(choice) => void choose(choice)}
    />
  );

  return { request, open: setRequest, dialog };
}
