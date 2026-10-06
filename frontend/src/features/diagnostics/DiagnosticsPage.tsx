import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  Body1,
  Button,
  Caption1,
  Card,
  Spinner,
  Subtitle2,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  ArrowClockwise20Regular,
  CheckmarkCircle20Filled,
  Copy20Regular,
  ErrorCircle20Filled,
  FolderOpen20Regular,
  Info20Regular,
  Settings20Regular,
  TextBulletListLtr20Regular,
  Warning20Filled,
} from "@fluentui/react-icons";
import { Events as WailsEvents } from "@wailsio/runtime";
import { useTranslation } from "react-i18next";
import { DiagService, errorOf, Events, type DiagItem, type ErrorView } from "../../api/backend";
import { ErrorBar, useNotify } from "../../components/Feedback";
import { Page } from "../../components/Page";
import { copyText } from "../../lib/clipboard";
import { AppLog } from "./AppLog";
import { useEditDefaults } from "./editDefaults";
import { byGroup, detailText, reportText, valueText } from "./report";

const useStyles = makeStyles({
  card: {
    padding: "16px 20px 20px",
    display: "flex",
    flexDirection: "column",
    gap: "14px",
  },
  items: {
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  item: {
    display: "grid",
    gridTemplateColumns: "20px 170px minmax(0, 1fr)",
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
  info: { color: tokens.colorNeutralForeground3 },
  warn: { color: tokens.colorStatusWarningForeground1 },
  error: { color: tokens.colorStatusDangerForeground1 },
  label: {
    color: tokens.colorNeutralForeground2,
  },
  value: {
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    minWidth: 0,
    wordBreak: "break-word",
  },
  detail: {
    color: tokens.colorNeutralForeground3,
  },
  actions: {
    display: "flex",
    gap: "8px",
  },
  center: {
    display: "flex",
    justifyContent: "center",
    padding: "32px",
  },
});

/**
 * What on this computer affects the connections: versions, Remote Desktop's
 * default settings, the policies on saved passwords. Read anew each time the
 * page opens or the user asks; nothing is changed.
 */
export function DiagnosticsPage() {
  const styles = useStyles();
  const { t } = useTranslation();
  const notify = useNotify();
  const editDefaults = useEditDefaults();
  const [items, setItems] = useState<DiagItem[] | null>(null);
  const [error, setError] = useState<ErrorView | null>(null);
  const [loading, setLoading] = useState(false);
  const [showLog, setShowLog] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setItems((await DiagService.Report()) ?? []);
      setError(null);
    } catch (e) {
      setError(errorOf(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // A part that takes long (whether Credential Guard runs) arrives later,
  // with the whole report.
  useEffect(
    () =>
      WailsEvents.On(Events.diagChanged, (event) => {
        setItems((event.data as DiagItem[] | null) ?? []);
      }),
    [],
  );

  const copy = async () => {
    if (!items) return;
    try {
      await copyText(reportText(t, items));
      notify.success(t("diag.copied"));
    } catch (e) {
      notify.error(e, t("diag.copyFailed"));
    }
  };

  const openLogs = async () => {
    try {
      await DiagService.OpenLogs();
    } catch (e) {
      notify.error(e, t("diag.openLogsFailed"));
    }
  };

  // Each group's way to change what it reports.
  const groupActions: Record<string, ReactNode> = {
    defaults: (
      <Button icon={<Settings20Regular />} onClick={editDefaults}>
        {t("diag.editDefaults")}
      </Button>
    ),
    files: (
      <>
        <Button icon={<TextBulletListLtr20Regular />} onClick={() => setShowLog(true)}>
          {t("diag.showLog")}
        </Button>
        <Button icon={<FolderOpen20Regular />} onClick={() => void openLogs()}>
          {t("diag.openLogs")}
        </Button>
      </>
    ),
  };

  return (
    <Page
      title={t("diag.title")}
      subtitle={t("diag.subtitle")}
      actions={
        <>
          <Button icon={<ArrowClockwise20Regular />} disabled={loading} onClick={() => void load()}>
            {t("diag.refresh")}
          </Button>
          <Button icon={<Copy20Regular />} disabled={!items || items.length === 0} onClick={() => void copy()}>
            {t("diag.copy")}
          </Button>
        </>
      }
    >
      <ErrorBar error={error} />
      {!items && !error && (
        <div className={styles.center}>
          <Spinner label={t("common.loading")} />
        </div>
      )}
      {items &&
        byGroup(items).map(([group, list]) => (
          <Card key={group} className={styles.card}>
            <Subtitle2 as="h2">{t(`diag.groups.${group}`)}</Subtitle2>
            <div className={styles.items}>
              {list.map((it) => (
                <ItemRow key={it.key} item={it} />
              ))}
            </div>
            {groupActions[group] && <div className={styles.actions}>{groupActions[group]}</div>}
          </Card>
        ))}
      {showLog && <AppLog onClose={() => setShowLog(false)} />}
    </Page>
  );
}

function ItemRow({ item }: { item: DiagItem }) {
  const styles = useStyles();
  const { t } = useTranslation();
  let icon: ReactNode;
  switch (item.status) {
    case "ok":
      icon = <CheckmarkCircle20Filled className={styles.ok} />;
      break;
    case "warn":
      icon = <Warning20Filled className={styles.warn} />;
      break;
    case "error":
      icon = <ErrorCircle20Filled className={styles.error} />;
      break;
    default:
      icon = <Info20Regular className={styles.info} />;
  }
  const detail = detailText(t, item);
  return (
    <div className={styles.item}>
      <span className={styles.icon} role="img" aria-label={t(`diag.statuses.${item.status}`, { defaultValue: item.status })}>
        {icon}
      </span>
      <Body1 className={styles.label}>{t(`diag.items.${item.key}.label`)}</Body1>
      <div className={styles.value}>
        <Body1>{valueText(t, item)}</Body1>
        {detail && <Caption1 className={styles.detail}>{detail}</Caption1>}
      </div>
    </div>
  );
}
