import { useState } from "react";
import {
  Button,
  Caption1,
  Checkbox,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Field,
  Input,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { useTranslation } from "react-i18next";
import type { ProfileView } from "../../api/backend";

const useStyles = makeStyles({
  content: {
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  hint: {
    color: tokens.colorNeutralForeground3,
  },
  actions: {
    width: "100%",
    display: "flex",
    justifyContent: "space-between",
    gap: "8px",
  },
  right: {
    display: "flex",
    gap: "8px",
  },
});

/**
 * Asked before connecting a profile that has no saved password. Leaving the
 * password empty lets Remote Desktop ask for it itself.
 */
export function PasswordDialog({
  view,
  onClose,
  onConnect,
}: {
  view: ProfileView;
  onClose: () => void;
  onConnect: (password: string, remember: boolean) => void;
}) {
  const styles = useStyles();
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const [remember, setRemember] = useState(view.profile.rememberPassword);

  return (
    <Dialog open onOpenChange={(_, d) => !d.open && onClose()}>
      <DialogSurface>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            onConnect(password, remember);
          }}
        >
          <DialogBody>
            <DialogTitle>{t("connections.password.title", { name: view.profile.name })}</DialogTitle>
            <DialogContent className={styles.content}>
              <Field label={t("connections.password.label", { user: view.profile.username })}>
                <Input type="password" value={password} autoFocus onChange={(_, d) => setPassword(d.value)} />
              </Field>
              <Checkbox checked={remember} label={t("connections.form.remember")} onChange={(_, d) => setRemember(!!d.checked)} />
              <Caption1 className={styles.hint}>{t("connections.password.hint")}</Caption1>
            </DialogContent>
            <DialogActions fluid className={styles.actions}>
              <Button appearance="subtle" type="button" onClick={() => onConnect("", remember)}>
                {t("connections.password.skip")}
              </Button>
              <div className={styles.right}>
                <Button appearance="secondary" type="button" onClick={onClose}>
                  {t("common.cancel")}
                </Button>
                <Button appearance="primary" type="submit" disabled={password === ""}>
                  {t("connections.connect")}
                </Button>
              </div>
            </DialogActions>
          </DialogBody>
        </form>
      </DialogSurface>
    </Dialog>
  );
}
