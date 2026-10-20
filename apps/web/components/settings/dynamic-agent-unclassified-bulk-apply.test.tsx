import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DynamicUnclassifiedBulkApply } from "./dynamic-agent-unclassified-bulk-apply";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

describe("DynamicUnclassifiedBulkApply", () => {
  afterEach(cleanup);

  it("confirms before applying to every candidate", () => {
    const onApply = vi.fn();
    render(<DynamicUnclassifiedBulkApply onApply={onApply} />);

    fireEvent.click(screen.getByTestId("dynamic-unclassified-apply-all"));
    expect(onApply).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("dynamic-unclassified-apply-all-confirm"));
    expect(onApply).toHaveBeenCalledTimes(1);
  });

  it("does not apply when the confirmation is cancelled", () => {
    const onApply = vi.fn();
    render(<DynamicUnclassifiedBulkApply onApply={onApply} />);

    fireEvent.click(screen.getByTestId("dynamic-unclassified-apply-all"));
    fireEvent.click(screen.getByRole("button", { name: "common:cancel" }));

    expect(onApply).not.toHaveBeenCalled();
  });
});
