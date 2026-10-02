import { useEffect } from "react";
import { FluentProvider, MessageBar, MessageBarBody, Spinner, makeStyles } from "@fluentui/react-components";
import { useTranslation } from "react-i18next";
import { useSettings } from "../stores/settings";
import { darkTheme, lightTheme, useSystemDark } from "./theme";
import { LanguageSetup } from "./LanguageSetup";
import { Shell } from "./Shell";

const useStyles = makeStyles({
  center: {
    height: "100%",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    padding: "24px",
    boxSizing: "border-box",
  },
});

export function App() {
  const styles = useStyles();
  const { t } = useTranslation();
  const status = useSettings((s) => s.status);
  const settings = useSettings((s) => s.settings);
  const loadError = useSettings((s) => s.loadError);
  const load = useSettings((s) => s.load);
  const systemDark = useSystemDark();

  useEffect(() => {
    void load();
  }, [load]);

  const theme = settings?.theme ?? "system";
  const dark = theme === "dark" || (theme === "system" && systemDark);

  let body;
  if (status === "error") {
    body = (
      <div className={styles.center}>
        <MessageBar intent="error">
          <MessageBarBody>{t("common.loadFailed", { message: loadError })}</MessageBarBody>
        </MessageBar>
      </div>
    );
  } else if (status === "loading" || !settings) {
    body = (
      <div className={styles.center}>
        <Spinner label={t("common.loading")} />
      </div>
    );
  } else if (settings.language === "") {
    body = <LanguageSetup />;
  } else {
    body = <Shell />;
  }

  return (
    // No className here: Fluent copies it onto popup portals (see styles.css).
    <FluentProvider theme={dark ? darkTheme : lightTheme}>{body}</FluentProvider>
  );
}
