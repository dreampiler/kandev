"use client";

import { Separator } from "@kandev/ui/separator";

import { WorkspaceRuntimeFootprintCard } from "@/components/settings/workspace-runtime-footprint-card";

/**
 * The workspace's live agent-runtime footprint, kept beside the idle-suspension
 * policy because that is the setting an operator checks when they suspect a
 * workspace is holding memory it is not using.
 *
 * This is a read-only observation. It never participates in the form's save
 * coordination, so it is factored out rather than inline: it would otherwise
 * push the settings form past the per-function line limit for something that
 * owns no draft state.
 */
export function WorkspaceRuntimeFootprintSection({ workspaceId }: { workspaceId: string }) {
  return (
    <>
      <Separator />
      <WorkspaceRuntimeFootprintCard workspaceId={workspaceId} />
    </>
  );
}
