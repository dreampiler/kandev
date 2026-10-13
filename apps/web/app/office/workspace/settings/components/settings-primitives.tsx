"use client";

import { Switch } from "@kandev/ui/switch";

export function SectionHeader({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 mb-3">
      <h2 className="text-[10px] font-medium uppercase tracking-widest font-mono text-muted-foreground/60 shrink-0">
        {children}
      </h2>
      <div className="h-px bg-border flex-1" />
    </div>
  );
}

export function SettingCard({ children }: { children: React.ReactNode }) {
  return <div className="rounded-lg border border-border p-4 space-y-4">{children}</div>;
}

export function ToggleRow({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string;
  description?: string;
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <div>
        <p className="text-sm">{label}</p>
        {description && <p className="text-xs text-muted-foreground mt-0.5">{description}</p>}
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} className="cursor-pointer" />
    </div>
  );
}
