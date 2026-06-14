import { afterEach, describe, expect, it, vi } from "vitest";
import {
  expandedKey,
  loadFlag,
  loadString,
  loadStringArray,
  pinsKey,
  runtimeKey,
  saveFlag,
  saveString,
  saveStringArray,
} from "./storage";

afterEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

describe("key builders", () => {
  it("namespace by target id", () => {
    expect(expandedKey("svc")).toBe("ml-expanded:svc");
    expect(pinsKey("svc")).toBe("ml-pins:svc");
    expect(runtimeKey("svc")).toBe("ml-runtime:svc");
  });
});

describe("string round-trips", () => {
  it("stores and reads a value, null when absent", () => {
    expect(loadString("missing")).toBeNull();
    saveString("k", "v");
    expect(loadString("k")).toBe("v");
  });

  it("swallows quota/private-mode errors on write", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("QuotaExceeded");
    });
    expect(() => {
      saveString("k", "v");
    }).not.toThrow();
  });
});

describe("string-array round-trips", () => {
  it("round-trips an array", () => {
    saveStringArray("k", ["a", "b"]);
    expect(loadStringArray("k")).toEqual(["a", "b"]);
  });

  it("returns [] for missing, malformed, or non-string entries", () => {
    expect(loadStringArray("missing")).toEqual([]);
    window.localStorage.setItem("bad", "{not json");
    expect(loadStringArray("bad")).toEqual([]);
    window.localStorage.setItem("obj", JSON.stringify({ a: 1 }));
    expect(loadStringArray("obj")).toEqual([]);
    window.localStorage.setItem("mixed", JSON.stringify(["a", 1, null, "b"]));
    expect(loadStringArray("mixed")).toEqual(["a", "b"]);
  });
});

describe("flags", () => {
  it("encodes booleans as 1/0 and reads them back", () => {
    saveFlag("k", true);
    expect(window.localStorage.getItem("k")).toBe("1");
    expect(loadFlag("k")).toBe(true);
    saveFlag("k", false);
    expect(loadFlag("k")).toBe(false);
  });

  it("is false for any non-'1' value", () => {
    expect(loadFlag("missing")).toBe(false);
    window.localStorage.setItem("k", "true");
    expect(loadFlag("k")).toBe(false);
  });
});
