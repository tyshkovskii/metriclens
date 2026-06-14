import { describe, expect, it } from "vitest";
import {
  clockTime,
  compactNumber,
  formatNumber,
  labelsText,
  sampleKey,
  shortDuration,
  shortTime,
} from "./format";

describe("labelsText", () => {
  it("is empty for a label-less sample", () => {
    expect(labelsText({})).toBe("");
  });

  it("renders labels sorted by key in prometheus form", () => {
    expect(labelsText({ method: "GET", code: "200" })).toBe('{code="200",method="GET"}');
  });
});

describe("sampleKey", () => {
  it("joins metric name with its label text", () => {
    expect(sampleKey("http_requests", { code: "200" })).toBe('http_requests{code="200"}');
    expect(sampleKey("up", {})).toBe("up");
  });
});

describe("shortDuration", () => {
  it("renders seconds-only for sub-minute spans", () => {
    expect(shortDuration(5_000)).toBe("5s");
  });

  it("renders minutes and zero-padded seconds", () => {
    expect(shortDuration(125_000)).toBe("2m 05s");
  });

  it("renders hours and zero-padded minutes past an hour", () => {
    expect(shortDuration(3_660_000)).toBe("1h 01m");
  });

  it("clamps negatives to 0s", () => {
    expect(shortDuration(-5_000)).toBe("0s");
  });
});

describe("clockTime / shortTime", () => {
  it("falls back to the raw input for an unparseable date", () => {
    expect(clockTime("not-a-date")).toBe("not-a-date");
    expect(shortTime("nope")).toBe("nope");
  });

  it("formats a valid timestamp as 24-hour time", () => {
    // Locale-dependent separators, so assert structure rather than exact glyphs.
    expect(clockTime(new Date("2024-01-01T13:45:30Z").getTime())).toMatch(/\d{1,2}.\d{2}.\d{2}/);
    expect(shortTime(new Date("2024-01-01T13:45:30Z").getTime())).toMatch(/\d{1,2}.\d{2}/);
  });
});

describe("formatNumber / compactNumber", () => {
  it("passes non-finite values through as strings", () => {
    expect(formatNumber(Number.NaN)).toBe("NaN");
    expect(formatNumber(Number.POSITIVE_INFINITY)).toBe("Infinity");
    expect(compactNumber(Number.NEGATIVE_INFINITY)).toBe("-Infinity");
  });

  it("formats finite numbers", () => {
    expect(formatNumber(1000)).toMatch(/1.?000/);
    expect(compactNumber(1500)).toMatch(/1\.5K/i);
  });
});
