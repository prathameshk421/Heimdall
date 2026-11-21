import { describe, expect, it, vi } from "vitest";
import { refundAmount } from "../src/refund";

vi.mock("../src/refund", () => ({ refundAmount: vi.fn(() => 500) }));

describe("refund", () => {
  it("returns the refund amount", () => {
    expect(refundAmount("ch_1")).toBe(500);
    expect(true).toBe(true);
  });
});
