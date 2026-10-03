"use client";

import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Drawer, DrawerContent, DrawerHeader, DrawerTitle } from "@kandev/ui/drawer";
import { Button } from "@kandev/ui/button";
import { settingsActionClassName } from "./settings-control";

type DynamicAgentInsetSurfaceProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  testId: string;
  children: ReactNode;
};

/**
 * The phone detail surface follows the shared menu geometry: an inset sheet with a
 * fixed header, one `min-h-0` scroll body, and a bottom safe area. The body is the
 * only vertical scroll owner on the surface, and the action row stays reachable
 * without scrolling to the end of a long localized form.
 */
export function DynamicAgentInsetSurface({
  open,
  onOpenChange,
  title,
  testId,
  children,
}: DynamicAgentInsetSurfaceProps) {
  const { t } = useTranslation();
  return (
    <Drawer open={open} onOpenChange={onOpenChange}>
      <DrawerContent
        className="h-[calc(100dvh-16px-env(safe-area-inset-bottom,0px))] !max-h-[calc(100dvh-16px-env(safe-area-inset-bottom,0px))] outline-none"
        data-testid={testId}
      >
        <div
          className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl bg-background shadow-2xl shadow-black/20"
          data-testid={`${testId}-card`}
        >
          <DrawerHeader className="shrink-0 border-b border-border/70 pb-3 text-left">
            <DrawerTitle className="min-w-0 truncate text-base">{title}</DrawerTitle>
          </DrawerHeader>
          <div
            className="min-h-0 flex-1 overflow-y-auto overscroll-contain"
            data-testid={`${testId}-scroll`}
          >
            <div className="flex min-h-full flex-col gap-4 p-4">{children}</div>
          </div>
          <div
            className="shrink-0 border-t border-border/70 px-4 py-3 pb-[calc(0.75rem+env(safe-area-inset-bottom,0px))]"
            data-testid={`${testId}-actions`}
          >
            <Button
              variant="outline"
              className={settingsActionClassName("w-full cursor-pointer")}
              onClick={() => onOpenChange(false)}
            >
              {t("agents:done")}
            </Button>
          </div>
        </div>
      </DrawerContent>
    </Drawer>
  );
}
