import { useEffect, useState, type ReactElement } from "react";
import { Button, MessageBar, MessageBarBody, Spinner, Subtitle2, makeStyles, mergeClasses, tokens } from "@fluentui/react-components";
import {
  Desktop24Regular,
  DesktopFilled,
  Globe24Regular,
  Power24Regular,
  Settings24Regular,
  Stethoscope24Regular,
} from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { AppToaster, useNotify } from "../components/Feedback";
import { useData } from "../stores/data";
import { Notices } from "./Notices";
import { QuitDialog } from "./QuitDialog";
import { ConnectionsPage } from "../features/connections/ConnectionsPage";
import { DiagnosticsPage } from "../features/diagnostics/DiagnosticsPage";
import { ProxiesPage } from "../features/proxies/ProxiesPage";
import { SettingsPage } from "../features/settings/SettingsPage";

type Page = "connections" | "proxies" | "diagnostics" | "settings";

const useStyles = makeStyles({
  root: {
    height: "100%",
    display: "grid",
    gridTemplateColumns: "240px minmax(0, 1fr)",
    backgroundColor: tokens.colorNeutralBackground1,
  },
  sidebar: {
    display: "flex",
    flexDirection: "column",
    gap: "4px",
    padding: "16px 12px",
    backgroundColor: tokens.colorNeutralBackground2,
    borderRight: `1px solid ${tokens.colorNeutralStroke2}`,
  },
  brand: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    padding: "4px 8px 16px",
  },
  brandIcon: {
    fontSize: "24px",
    color: tokens.colorBrandForeground1,
  },
  navItem: {
    position: "relative",
    justifyContent: "flex-start",
    width: "100%",
    fontWeight: tokens.fontWeightRegular,
  },
  // Like the Windows 11 Settings app: a filled row plus a short accent pill on
  // the left, so the current page stands out in both light and dark themes.
  navItemSelected: {
    backgroundColor: tokens.colorSubtleBackgroundSelected,
    fontWeight: tokens.fontWeightSemibold,
    "::before": {
      content: '""',
      position: "absolute",
      left: "0",
      top: "25%",
      bottom: "25%",
      width: "3px",
      borderRadius: "2px",
      backgroundColor: tokens.colorCompoundBrandBackground,
    },
    "& .fui-Button__icon": {
      color: tokens.colorCompoundBrandForeground1,
    },
  },
  spacer: {
    flexGrow: 1,
  },
  content: {
    overflowY: "auto",
    minHeight: 0,
  },
  center: {
    padding: "48px",
    display: "flex",
    justifyContent: "center",
  },
});

export function Shell() {
  const styles = useStyles();
  const { t } = useTranslation();
  const [page, setPage] = useState<Page>("connections");
  const notify = useNotify();
  const status = useData((s) => s.status);
  const loadError = useData((s) => s.loadError);
  const load = useData((s) => s.load);
  const quit = useData((s) => s.quit);

  useEffect(() => {
    void load();
  }, [load]);

  const navItem = (id: Page, icon: ReactElement, label: string) => (
    <Button
      appearance="subtle"
      icon={icon}
      className={mergeClasses(styles.navItem, page === id && styles.navItemSelected)}
      aria-current={page === id ? "page" : undefined}
      onClick={() => setPage(id)}
    >
      {label}
    </Button>
  );

  return (
    <div className={styles.root}>
      <nav className={styles.sidebar}>
        <div className={styles.brand}>
          <DesktopFilled className={styles.brandIcon} />
          <Subtitle2>{t("app.name")}</Subtitle2>
        </div>
        {navItem("connections", <Desktop24Regular />, t("nav.connections"))}
        {navItem("proxies", <Globe24Regular />, t("nav.proxies"))}
        <div className={styles.spacer} />
        {navItem("diagnostics", <Stethoscope24Regular />, t("nav.diagnostics"))}
        {navItem("settings", <Settings24Regular />, t("nav.settings"))}
        <Button
          appearance="subtle"
          icon={<Power24Regular />}
          className={styles.navItem}
          onClick={() => quit().catch((e: unknown) => notify.error(e))}
        >
          {t("nav.quit")}
        </Button>
      </nav>
      <main className={styles.content}>
        <Notices />
        {status === "loading" && (
          <div className={styles.center}>
            <Spinner label={t("common.loading")} />
          </div>
        )}
        {status === "error" && (
          <div className={styles.center}>
            <MessageBar intent="error">
              <MessageBarBody>{t("common.dataLoadFailed", { message: loadError })}</MessageBarBody>
            </MessageBar>
          </div>
        )}
        {status === "ready" && page === "connections" && <ConnectionsPage />}
        {status === "ready" && page === "proxies" && <ProxiesPage />}
        {page === "diagnostics" && <DiagnosticsPage />}
        {page === "settings" && <SettingsPage />}
      </main>
      <QuitDialog />
      <AppToaster />
    </div>
  );
}
