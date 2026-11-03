import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import { fetchConfig, fetchSeriesByMetric, fetchTargets, loadTargetData } from "./api";
import type { Target } from "./types";

vi.mock("./api", () => ({
  fetchConfig: vi.fn(),
  fetchSeriesByMetric: vi.fn(),
  fetchTargets: vi.fn(),
  loadTargetData: vi.fn(),
}));
vi.mock("./components/PanelChart", () => ({
  ChartBody: () => <div>metric chart</div>,
  KindLabel: () => null,
  PanelChart: () => null,
}));

const targets: Target[] = ["api", "worker"].map((id) => ({
  id,
  serviceName: id,
  containerName: id,
  status: "up",
  discoveredAt: "2026-11-02T00:00:00Z",
}));

beforeEach(() => {
  window.localStorage.clear();
  window.history.replaceState(null, "", "/");
  vi.mocked(fetchConfig).mockResolvedValue({ scrapeIntervalMs: 5000, retentionMs: 900_000 });
  vi.mocked(fetchTargets).mockResolvedValue(targets);
  vi.mocked(fetchSeriesByMetric).mockResolvedValue({});
  vi.mocked(loadTargetData).mockImplementation((id) => {
    const target = targets.find((candidate) => candidate.id === id);
    if (!target) throw new Error(`Unexpected target ${id}`);
    return Promise.resolve({
      metrics: {
        target,
        families: [
          {
            name: "queue_depth",
            type: "gauge",
            samples: [{ metric: "queue_depth", labels: {}, value: null }],
          },
        ],
      },
      panels: [],
      issues: [],
    });
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("agent-to-human navigation", () => {
  it("opens the requested service and metric rather than the first service", async () => {
    window.history.replaceState(
      null,
      "",
      "/?target=worker&metric=queue_depth&start=2026-11-02T18%3A00%3A00Z&end=2026-11-02T18%3A01%3A00Z",
    );
    render(<App />);
    await screen.findByText("metric chart");
    expect(screen.getByRole("button", { name: /worker/ })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("searchbox")).toHaveValue("queue_depth");
    expect(screen.getByRole("button", { name: /queue_depth.*gauge/ })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    expect(screen.getByText(/Viewing samples from 2026-11-02T18:00:00.000Z/)).toBeInTheDocument();
    const link = new URL(screen.getByRole<HTMLAnchorElement>("link", { name: "Link to queue_depth" }).href);
    expect(link.searchParams.get("target")).toBe("worker");
    expect(link.searchParams.get("start")).toBe("2026-11-02T18:00:00.000Z");
  });

  it("does not substitute a different service when a linked target disappeared", async () => {
    window.history.replaceState(null, "", "/?target=missing&metric=queue_depth");
    render(<App />);
    expect(await screen.findByText(/Linked target “missing” is no longer available/)).toBeInTheDocument();
    expect(loadTargetData).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: /worker/ }));
    await screen.findByRole("searchbox");
    expect(window.location.hash).toBe("#worker");
    expect(window.location.search).toBe("");
  });

  it("updates the metric when browser navigation returns to a link", async () => {
    render(<App />);
    await screen.findByRole("searchbox");
    act(() => {
      window.history.pushState(null, "", "/?target=worker&metric=queue_depth");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(() => expect(screen.getByRole("searchbox")).toHaveValue("queue_depth"));
    expect(screen.getByRole("button", { name: /worker/ })).toHaveAttribute("aria-current", "page");
  });

  it("leaves native Tab navigation available", async () => {
    render(<App />);
    await screen.findByRole("searchbox");
    const event = new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true });
    window.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
  });
});
