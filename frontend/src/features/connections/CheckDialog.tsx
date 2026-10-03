import { useEffect, useState, type ReactNode } from "react";
import {
  Body1,
  Button,
  Caption1,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  MessageBar,
  MessageBarBody,
  Spinner,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  CheckmarkCircle20Filled,
  DismissCircle20Filled,
  SubtractCircle20Regular,
  Warning20Filled,
} from "@fluentui/react-icons";
import type { CancellablePromise } from "@wailsio/runtime";
import { useTranslation } from "react-i18next";
import { DIRECT_PROXY_ID, errorOf, ProxyService, RDP_PORT, SessionService, type ProfileView } from "../../api/backend";
import { joinHostPort } from "../../lib/address";
import { errorDetails, errorText } from "../../lib/messages";
import { useData } from "../../stores/data";
import { useSettings } from "../../stores/settings";
import { proxyName } from "../proxies/names";
import { protocolInfo, running, startTest, verdict, type RouteTest, type Step, type Tone } from "./routeTest";

const useStyles = makeStyles({
  surface: {
    width: "560px",
    maxWidth: "calc(100vw - 48px)",
  },
  content: {
    display: "flex",
    flexDirection: "column",
    gap: "16px",
  },
  route: {
    color: tokens.colorNeutralForeground3,
    wordBreak: "break-word",
  },
  steps: {
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  step: {
    display: "grid",
    gridTemplateColumns: "20px minmax(0, 1fr) auto",
    columnGap: "10px",
    alignItems: "start",
  },
  icon: {
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    height: "20px",
    fontSize: "20px",
  },
  ok: { color: tokens.colorPaletteGreenForeground1 },
  warn: { color: tokens.colorStatusWarningForeground1 },
  error: { color: tokens.colorStatusDangerForeground1 },
  muted: { color: tokens.colorNeutralForeground3 },
  text: {
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    minWidth: 0,
  },
  details: {
    color: tokens.colorNeutralForeground3,
    wordBreak: "break-word",
  },
  ms: {
    color: tokens.colorNeutralForeground3,
    whiteSpace: "nowrap",
  },
});

/** The host of the test URL, to say where the proxy test goes. */
function urlHost(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

/**
 * Tests a profile's route without starting Remote Desktop, step by step:
 * the proxy on its own, the remote computer through it, and the security it
 * asks for. Both requests run at once and wait as long as the route takes;
 * closing the dialog cancels them.
 */
export function CheckDialog({ view, onClose }: { view: ProfileView; onClose: () => void }) {
  const styles = useStyles();
  const { t } = useTranslation();
  const p = view.profile;
  const proxy = useData((s) => s.proxies.find((x) => x.proxy.id === p.proxyId));
  const testUrl = useSettings((s) => s.settings?.testUrl ?? "");
  const direct = p.proxyId === DIRECT_PROXY_ID;
  const [round, setRound] = useState(0);
  const [test, setTest] = useState<RouteTest>(() => startTest(direct));
  const [started, setStarted] = useState(() => Date.now());
  const [now, setNow] = useState(started);
  const busy = running(test);

  useEffect(() => {
    const begin = Date.now();
    setStarted(begin);
    setNow(begin);
    setTest(startTest(direct));
    // Results of a round that was cancelled (closing, testing again, or
    // StrictMode's trial run) are dropped.
    let live = true;
    const calls: CancellablePromise<unknown>[] = [];
    const settle = (change: (s: RouteTest) => RouteTest) => {
      if (live) setTest(change);
    };
    if (!direct && view.proxyMissing) {
      settle((s) => ({ ...s, proxy: { kind: "failed", error: { code: "profile.proxyMissing", message: "" } } }));
    } else if (!direct) {
      const latency = ProxyService.Latency(p.proxyId);
      calls.push(latency);
      latency.then(
        (r) => settle((s) => ({ ...s, proxy: { kind: "passed", ms: r.ms } })),
        (e: unknown) => settle((s) => ({ ...s, proxy: { kind: "failed", error: errorOf(e) } })),
      );
    }
    const check = SessionService.CheckRoute(p.id);
    calls.push(check);
    check.then(
      (r) => settle((s) => ({ ...s, target: { kind: "passed", ms: r.elapsedMs }, check: r })),
      (e: unknown) => settle((s) => ({ ...s, target: { kind: "failed", error: errorOf(e) } })),
    );
    return () => {
      live = false;
      for (const c of calls) c.cancel();
    };
  }, [round, direct, p.id, p.proxyId, view.proxyMissing]);

  // Counts the seconds while the test runs. Display only: nothing waits on
  // it, and the test has no time limit.
  useEffect(() => {
    if (!busy) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [busy]);

  const target = joinHostPort(p.target.host, p.target.port, RDP_PORT);
  const result = verdict(test);
  const proxyLabel = proxy ? proxyName(t, proxy.proxy) : t("connections.proxyMissing");
  const info = test.check ? protocolInfo(test.check) : null;

  return (
    <Dialog open onOpenChange={(_, d) => !d.open && onClose()}>
      <DialogSurface className={styles.surface}>
        <DialogBody>
          <DialogTitle>{t("connections.test.title", { name: p.name })}</DialogTitle>
          <DialogContent className={styles.content}>
            <Caption1 className={styles.route}>
              {direct ? t("connections.test.routeDirect", { target }) : t("connections.test.route", { proxy: proxyLabel, target })}
            </Caption1>
            <div className={styles.steps}>
              <StepRow
                step={test.proxy}
                label={t("connections.test.proxy")}
                what={direct ? t("connections.test.proxySkipped") : t("connections.test.proxyWhat", { host: urlHost(testUrl) })}
              />
              <StepRow step={test.target} label={t("connections.test.target")} what={t("connections.test.targetWhat")} />
              <Row
                tone={info ? info.tone : test.target.kind === "failed" ? "skipped" : "waiting"}
                label={t("connections.test.security")}
              >
                <Caption1 className={info ? undefined : styles.muted}>
                  {info
                    ? t(info.key, { name: info.name })
                    : test.target.kind === "failed"
                      ? t("connections.test.securityUnknown")
                      : t("connections.test.securityWaiting")}
                </Caption1>
              </Row>
            </div>
            {busy && (
              <Caption1 className={styles.muted}>
                {t("connections.test.elapsed", { seconds: Math.floor((now - started) / 1000) })}
              </Caption1>
            )}
            {result && (
              <MessageBar
                intent={result.tone === "ok" ? "success" : result.tone === "warn" ? "warning" : "error"}
                layout="multiline"
              >
                <MessageBarBody>{t(result.key)}</MessageBarBody>
              </MessageBar>
            )}
          </DialogContent>
          <DialogActions>
            {!busy && <Button onClick={() => setRound((r) => r + 1)}>{t("connections.test.again")}</Button>}
            <Button appearance={busy ? "secondary" : "primary"} onClick={onClose}>
              {busy ? t("common.cancel") : t("common.close")}
            </Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}

type RowTone = Tone | "running" | "skipped" | "waiting";

/** One step: an icon for how it went, its name and what it found. */
function Row({ tone, label, aside, children }: { tone: RowTone; label: string; aside?: ReactNode; children: ReactNode }) {
  const styles = useStyles();
  let icon: ReactNode = null;
  switch (tone) {
    case "running":
      icon = <Spinner size="extra-tiny" />;
      break;
    case "ok":
      icon = <CheckmarkCircle20Filled className={styles.ok} />;
      break;
    case "warn":
      icon = <Warning20Filled className={styles.warn} />;
      break;
    case "error":
      icon = <DismissCircle20Filled className={styles.error} />;
      break;
    case "skipped":
      icon = <SubtractCircle20Regular className={styles.muted} />;
      break;
  }
  return (
    <div className={styles.step}>
      <span className={styles.icon}>{icon}</span>
      <div className={styles.text}>
        <Body1>
          <Text weight="semibold">{label}</Text>
        </Body1>
        {children}
      </div>
      {aside}
    </div>
  );
}

function StepRow({ step, label, what }: { step: Step; label: string; what: string }) {
  const styles = useStyles();
  const { t, i18n } = useTranslation();
  const tone: RowTone =
    step.kind === "passed" ? "ok" : step.kind === "failed" ? "error" : step.kind === "skipped" ? "skipped" : "running";
  const details = step.kind === "failed" ? errorDetails(step.error) : "";
  const text = step.kind === "failed" ? errorText(i18n, step.error) : "";
  return (
    <Row
      tone={tone}
      label={label}
      aside={step.kind === "passed" && <Caption1 className={styles.ms}>{t("connections.test.ms", { ms: step.ms })}</Caption1>}
    >
      <Caption1 className={styles.muted}>{what}</Caption1>
      {step.kind === "failed" && (
        <>
          <Caption1 className={styles.error}>{text}</Caption1>
          {details && details !== text && <Caption1 className={styles.details}>{details}</Caption1>}
        </>
      )}
    </Row>
  );
}
