import type { ReactNode } from "react";
import {
  Body1,
  Caption1,
  Card,
  Dropdown,
  Field,
  Link,
  MessageBar,
  MessageBarBody,
  Option,
  Radio,
  RadioGroup,
  Subtitle2,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Browser } from "@wailsio/runtime";
import { useTranslation } from "react-i18next";
import { Page } from "../../components/Page";
import { isLanguage, languages } from "../../i18n";
import { useSettings } from "../../stores/settings";

const useStyles = makeStyles({
  card: {
    padding: "16px 20px 20px",
    display: "flex",
    flexDirection: "column",
    gap: "16px",
  },
  hint: {
    color: tokens.colorNeutralForeground3,
  },
  dropdown: {
    minWidth: "240px",
  },
  about: {
    display: "grid",
    gridTemplateColumns: "max-content 1fr",
    columnGap: "24px",
    rowGap: "8px",
    alignItems: "baseline",
  },
  label: {
    color: tokens.colorNeutralForeground3,
  },
});

function Section({ title, children }: { title: string; children: ReactNode }) {
  const styles = useStyles();
  return (
    <Card className={styles.card}>
      <Subtitle2 as="h2">{title}</Subtitle2>
      {children}
    </Card>
  );
}

export function SettingsPage() {
  const styles = useStyles();
  const { t } = useTranslation();
  const settings = useSettings((s) => s.settings);
  const appInfo = useSettings((s) => s.appInfo);
  const save = useSettings((s) => s.save);
  const saveError = useSettings((s) => s.saveError);
  if (!settings) return null;

  return (
    <Page title={t("settings.title")} subtitle={t("settings.autosave")}>
      {saveError && (
        <MessageBar intent="error">
          <MessageBarBody>{t("settings.saveFailed", { message: saveError })}</MessageBarBody>
        </MessageBar>
      )}

      <Section title={t("settings.appearance")}>
        <Field label={t("settings.language")}>
          <Dropdown
            className={styles.dropdown}
            value={t(`languageName.${settings.language}`)}
            selectedOptions={[settings.language]}
            onOptionSelect={(_, data) => {
              if (data.optionValue && isLanguage(data.optionValue) && data.optionValue !== settings.language) {
                void save({ language: data.optionValue });
              }
            }}
          >
            {languages.map((lng) => (
              <Option key={lng} value={lng}>
                {t(`languageName.${lng}`)}
              </Option>
            ))}
          </Dropdown>
        </Field>
        <Field label={t("settings.theme")}>
          <RadioGroup layout="horizontal" value={settings.theme} onChange={(_, data) => void save({ theme: data.value })}>
            <Radio value="system" label={t("settings.themeSystem")} />
            <Radio value="light" label={t("settings.themeLight")} />
            <Radio value="dark" label={t("settings.themeDark")} />
          </RadioGroup>
        </Field>
      </Section>

      <Section title={t("settings.behavior")}>
        <Field label={t("settings.closeBehavior")}>
          <RadioGroup value={settings.closeBehavior} onChange={(_, data) => void save({ closeBehavior: data.value })}>
            <Radio value="tray" label={t("settings.closeToTray")} />
            <Radio value="quit" label={t("settings.closeQuit")} />
          </RadioGroup>
        </Field>
        <Caption1 className={styles.hint}>{t("settings.closeHint")}</Caption1>
      </Section>

      <Section title={t("settings.about")}>
        <div className={styles.about}>
          <Body1 className={styles.label}>{t("settings.version")}</Body1>
          <Body1>
            {appInfo?.name} {appInfo?.version}
          </Body1>
          <Body1 className={styles.label}>{t("settings.homepage")}</Body1>
          <Link onClick={() => appInfo && void Browser.OpenURL(appInfo.repo)}>{appInfo?.repo}</Link>
        </div>
        <Caption1 className={styles.hint}>{t("settings.license")}</Caption1>
      </Section>
    </Page>
  );
}
