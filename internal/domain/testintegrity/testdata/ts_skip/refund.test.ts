import { describe, expect, it, xit } from "vitest";
import { refundAmount } from "../src/refund";

describe.skip("refund edge cases", () => {
  it("rounds half up", () => {
    expect(refundAmount("ch_2")).toBe(501);
  });
});

xit("keeps the refund ref", () => {
  expect(refundAmount("ch_1")).toBe(500);
});
