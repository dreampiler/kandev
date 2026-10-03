"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  previewDynamicProfile,
  type DynamicPreviewResponse,
} from "@/lib/api/domains/dynamic-preview-api";
import { isHandledApiError } from "@/lib/api/client";

export type DynamicPreviewLoadState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready"; preview: DynamicPreviewResponse }
  | { status: "failed" };

export type DynamicPreviewController = {
  state: DynamicPreviewLoadState;
  refresh: () => void;
};

/**
 * Requests are keyed by the draft revision they were issued for, and a response
 * is applied only when its revision is still the current one. Editing the draft
 * while a preview is in flight therefore cannot leave an older prediction on
 * screen under newer settings.
 */
export function useDynamicSelectionPreview({
  payload,
  revision,
  profileId,
  enabled,
}: {
  payload: unknown;
  revision: string;
  profileId?: string;
  enabled: boolean;
}): DynamicPreviewController {
  const [state, setState] = useState<DynamicPreviewLoadState>({ status: "idle" });
  const currentRevision = useRef(revision);
  // A nonce, not the revision, drives an explicit re-check: setting the same
  // revision again would bail out of the state update and never re-request.
  const [nonce, setNonce] = useState(0);
  currentRevision.current = revision;

  useEffect(() => {
    if (!enabled) {
      setState({ status: "idle" });
      return;
    }
    let cancelled = false;
    setState({ status: "loading" });
    previewDynamicProfile(payload, { profileId })
      .then((preview) => {
        if (cancelled || currentRevision.current !== revision) return;
        setState({ status: "ready", preview });
      })
      .catch((error) => {
        if (cancelled || currentRevision.current !== revision) return;
        if (isHandledApiError(error)) return;
        setState({ status: "failed" });
      });
    return () => {
      cancelled = true;
    };
    // `payload` is derived from `revision`; depending on both would re-request on
    // every render because the object identity is new each time.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [revision, nonce, profileId, enabled]);

  const refresh = useCallback(() => setNonce((current) => current + 1), []);

  return { state, refresh };
}
