import { useState, type ReactElement } from "react";
import { Button, Subtitle2, makeStyles, mergeClasses, tokens } from "@fluentui/react-components";
import {
  Desktop24Regular,
  DesktopFilled,
  Globe24Regular,
  Settings24Regular,
} from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { ConnectionsPage } from "../features/connections/ConnectionsPage";
import { ProxiesPage } from "../features/proxies/ProxiesPage";
import { SettingsPage } from "../features/settings/SettingsPage";

type Page = "connections" | "proxies" | "settings";

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
});

export function Shell() {
  const styles = useStyles();
  const { t } = useTranslation();
  const [page, setPage] = useState<Page>("connections");

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
        {navItem("settings", <Settings24Regular />, t("nav.settings"))}
      </nav>
      <main className={styles.content}>
        {page === "connections" && <ConnectionsPage />}
        {page === "proxies" && <ProxiesPage />}
        {page === "settings" && <SettingsPage />}
      </main>
    </div>
  );
}
