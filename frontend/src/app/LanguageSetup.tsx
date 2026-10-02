import { useState } from "react";
import {
  Body1,
  Button,
  Caption1,
  Card,
  Dropdown,
  Field,
  MessageBar,
  MessageBarBody,
  Option,
  Title3,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { ArrowRight20Regular, LocalLanguage24Regular } from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { languages, isLanguage, type Language } from "../i18n";
import { useSettings } from "../stores/settings";

const useStyles = makeStyles({
  page: {
    height: "100%",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: tokens.colorNeutralBackground2,
  },
  card: {
    width: "420px",
    padding: "32px",
    display: "flex",
    flexDirection: "column",
    gap: "16px",
  },
  icon: {
    fontSize: "32px",
    color: tokens.colorBrandForeground1,
  },
  titles: {
    display: "flex",
    flexDirection: "column",
    gap: "2px",
  },
  secondary: {
    color: tokens.colorNeutralForeground3,
  },
  actions: {
    display: "flex",
    justifyContent: "flex-end",
  },
});

/**
 * First-run language picker. Every text is shown in both languages, because
 * the user has not told us which one they read yet.
 */
export function LanguageSetup() {
  const styles = useStyles();
  const { t } = useTranslation();
  const systemLanguage = useSettings((s) => s.systemLanguage);
  const save = useSettings((s) => s.save);
  const saveError = useSettings((s) => s.saveError);
  const [choice, setChoice] = useState<Language>(systemLanguage);
  const [saving, setSaving] = useState(false);

  // The suggested language first, the other one second.
  const order: Language[] = [choice, ...languages.filter((l) => l !== choice)];
  const both = (key: string) => order.map((lng) => t(key, { lng }));

  const confirm = async () => {
    setSaving(true);
    await save({ language: choice });
    setSaving(false);
  };

  const [title, subtitle] = both("languageSetup.title");
  return (
    <div className={styles.page}>
      <Card className={styles.card}>
        <LocalLanguage24Regular className={styles.icon} />
        <div className={styles.titles}>
          <Title3>{title}</Title3>
          <Body1 className={styles.secondary}>{subtitle}</Body1>
        </div>
        <Field label={both("languageSetup.label").join(" / ")}>
          <Dropdown
            value={t(`languageName.${choice}`)}
            selectedOptions={[choice]}
            onOptionSelect={(_, data) => {
              if (data.optionValue && isLanguage(data.optionValue)) setChoice(data.optionValue);
            }}
          >
            {languages.map((lng) => (
              <Option key={lng} value={lng}>
                {t(`languageName.${lng}`)}
              </Option>
            ))}
          </Dropdown>
        </Field>
        {saveError && (
          <MessageBar intent="error">
            <MessageBarBody>{t("settings.saveFailed", { lng: choice, message: saveError })}</MessageBarBody>
          </MessageBar>
        )}
        <div className={styles.actions}>
          <Button appearance="primary" icon={<ArrowRight20Regular />} iconPosition="after" disabled={saving} onClick={confirm}>
            {both("languageSetup.continue").join(" / ")}
          </Button>
        </div>
        <Caption1 className={styles.secondary}>{both("languageSetup.hint").join(" ")}</Caption1>
      </Card>
    </div>
  );
}
