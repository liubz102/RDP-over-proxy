import { useEffect, useLayoutEffect, useRef } from "react";
import {
  Body1,
  Button,
  Caption1,
  DrawerBody,
  DrawerHeader,
  DrawerHeaderTitle,
  OverlayDrawer,
  makeStyles,
  mergeClasses,
  tokens,
} from "@fluentui/react-components";
import { Dismiss24Regular } from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import type { LogLine, ProfileView } from "../../api/backend";
import { logDetails, logText } from "../../lib/messages";
import { useData } from "../../stores/data";

const useStyles = makeStyles({
  facts: {
    display: "grid",
    gridTemplateColumns: "max-content 1fr",
    columnGap: "16px",
    rowGap: "4px",
    padding: "12px",
    marginBottom: "12px",
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorNeutralBackground2,
  },
  label: {
    color: tokens.colorNeutralForeground3,
  },
  lines: {
    display: "flex",
    flexDirection: "column",
    gap: "6px",
  },
  line: {
    display: "grid",
    gridTemplateColumns: "max-content 1fr",
    columnGap: "10px",
    alignItems: "baseline",
  },
  time: {
    fontFamily: tokens.fontFamilyMonospace,
    color: tokens.colorNeutralForeground3,
  },
  warn: {
    color: tokens.colorStatusWarningForeground1,
  },
  error: {
    color: tokens.colorStatusDangerForeground1,
  },
  details: {
    gridColumn: "2",
    color: tokens.colorNeutralForeground3,
    wordBreak: "break-word",
  },
  empty: {
    color: tokens.colorNeutralForeground3,
  },
});

function clock(time: string): string {
  const d = new Date(time);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString(undefined, { hour12: false });
}

/** The latest session's log of one profile, kept up to date while open. */
export function SessionLog({ view, onClose }: { view: ProfileView; onClose: () => void }) {
  const styles = useStyles();
  const { t, i18n } = useTranslation();
  const id = view.profile.id;
  const lines = useData((s) => s.logs[id]);
  const session = useData((s) => s.sessions[id]);
  const loadLog = useData((s) => s.loadLog);
  const end = useRef<HTMLDivElement>(null);

  useEffect(() => {
    void loadLog(id);
  }, [id, loadLog]);

  // Follow new lines.
  useLayoutEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [lines]);

  const fact = (label: string, value: string | number | undefined) =>
    value === undefined || value === "" ? null : (
      <>
        <Caption1 className={styles.label}>{label}</Caption1>
        <Caption1>{value}</Caption1>
      </>
    );

  const check = session?.check;
  return (
    <OverlayDrawer open position="end" size="medium" onOpenChange={(_, d) => !d.open && onClose()}>
      <DrawerHeader>
        <DrawerHeaderTitle
          action={<Button appearance="subtle" aria-label={t("common.close")} icon={<Dismiss24Regular />} onClick={onClose} />}
        >
          {t("connections.log.title", { name: view.profile.name })}
        </DrawerHeaderTitle>
      </DrawerHeader>
      <DrawerBody>
        {session && (
          <div className={styles.facts}>
            {fact(t("connections.log.phase"), t(`phases.${session.phase}`))}
            {fact(t("connections.log.entrance"), session.addr)}
            {fact(t("connections.log.pid"), session.pid || undefined)}
            {session.phase === "running" && fact(t("connections.log.conns"), session.conns)}
            {check &&
              fact(
                t("connections.log.check"),
                check.negotiationFailure
                  ? `${check.elapsedMs} ms · ${check.negotiationFailure}`
                  : `${check.elapsedMs} ms · ${check.protocol}`,
              )}
          </div>
        )}
        <div className={styles.lines} role="log" aria-label={t("connections.log.title", { name: view.profile.name })}>
          {(lines ?? []).length === 0 && <Body1 className={styles.empty}>{t("connections.log.empty")}</Body1>}
          {(lines ?? []).map((line: LogLine, i) => {
            const details = logDetails(i18n, line);
            const tone = line.level === "error" ? styles.error : line.level === "warn" ? styles.warn : undefined;
            return (
              <div key={`${line.time}-${i}`} className={styles.line}>
                <Caption1 className={styles.time}>{clock(line.time)}</Caption1>
                <Body1 className={mergeClasses(tone)}>{logText(i18n, line)}</Body1>
                {details && <Caption1 className={styles.details}>{details}</Caption1>}
              </div>
            );
          })}
          <div ref={end} />
        </div>
      </DrawerBody>
    </OverlayDrawer>
  );
}
