import { useTranslation } from "react-i18next";
import { ConfirmDialog } from "../components/Feedback";
import { useData } from "../stores/data";

/** Asks before quitting ends remote desktops; opened from the sidebar or the tray. */
export function QuitDialog() {
  const { t } = useTranslation();
  const count = useData((s) => s.quitConfirm);
  const confirmQuit = useData((s) => s.confirmQuit);
  return (
    <ConfirmDialog
      open={count !== null}
      title={t("quit.title")}
      confirm={t("quit.confirm")}
      danger
      onClose={() => confirmQuit(false)}
      onConfirm={() => confirmQuit(true)}
    >
      {t("quit.body", { count: count ?? 0 })}
    </ConfirmDialog>
  );
}
