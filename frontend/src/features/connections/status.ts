import type { ErrorView, SessionView } from "../../api/backend";

/** How a connection's row looks: its colour and what its buttons do. */
export type Tone = "idle" | "busy" | "ok" | "warn" | "error";

export interface Status {
  tone: Tone;
  /** Translation key of the one-line status, or null for none. */
  label: string | null;
  /** Arguments for the label. */
  args?: Record<string, string>;
  /** What went wrong, shown after the label. */
  error?: ErrorView | null;
  /** The buttons the row offers. */
  actions: "connect" | "cancel" | "running" | "ending";
}

/** What a profile's latest session amounts to in its row. */
export function sessionStatus(s: SessionView | undefined): Status {
  if (!s) return { tone: "idle", label: null, actions: "connect" };
  switch (s.phase) {
    case "preparing":
    case "checking":
    case "launching":
      return { tone: "busy", label: `status.${s.phase}`, actions: "cancel" };
    case "running":
      if (s.tunnelError) {
        return { tone: "error", label: "status.tunnelFailed", error: s.tunnelError, actions: "running" };
      }
      if (s.upstream === "failing") {
        return { tone: "warn", label: "status.upstreamFailing", error: s.upstreamError, actions: "running" };
      }
      if (s.upstream === "ok") return { tone: "ok", label: "status.connected", actions: "running" };
      return { tone: "ok", label: "status.running", actions: "running" };
    case "ending":
      return { tone: "busy", label: "status.ending", actions: "ending" };
    default:
      if (s.outcome === "failed") {
        return {
          tone: "error",
          label: "status.failed",
          args: { step: s.failedStep },
          error: s.failure,
          actions: "connect",
        };
      }
      return { tone: "idle", label: null, actions: "connect" };
  }
}
