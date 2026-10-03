import { cn } from "@/lib/utils";
import { controlSizingClassName } from "@kandev/ui/control-sizing";
import { SETTINGS_TYPOGRAPHY } from "./settings-typography";

/** Editable/select settings controls use the shared responsive size contract. */
export function settingsControlClassName(className?: string) {
  return cn(controlSizingClassName("standard"), SETTINGS_TYPOGRAPHY.control, className);
}

/** Credential and secret fields share the technical value treatment. */
export function settingsCredentialClassName(className?: string) {
  return settingsControlClassName(cn("font-mono", className));
}

/** Settings actions use the same responsive size contract as editable controls. */
export function settingsActionClassName(className?: string) {
  return cn(controlSizingClassName("standard"), SETTINGS_TYPOGRAPHY.mobileAction, className);
}

/**
 * A switch whose hit area meets the 44px touch minimum while the visible pill
 * keeps the ordinary settings density.
 *
 * `@kandev/ui/switch` renders a 16.6px-tall control, and a surrounding row's
 * `min-h-11` does not enlarge the control itself, so the switch has to carry the
 * size. The track is redrawn as a `before` pseudo-element so growing the element
 * does not stretch the visual pill.
 */
export function settingsTouchSwitchClassName(className?: string) {
  return cn(
    "cursor-pointer p-2",
    "data-[size=default]:h-11 data-[size=default]:w-11",
    "data-checked:bg-transparent data-unchecked:bg-transparent dark:data-unchecked:bg-transparent",
    "before:absolute before:left-2 before:top-1/2 before:h-[16.6px] before:w-7 before:-translate-y-1/2",
    "before:rounded-full before:bg-input before:content-['']",
    "data-checked:before:bg-primary dark:data-unchecked:before:bg-input/80",
    "[&_[data-slot=switch-thumb]]:z-10",
    className,
  );
}
