import { useTranslation } from "react-i18next";
import { DiagService } from "../../api/backend";
import { useNotify } from "../../components/Feedback";

/**
 * Opens Remote Desktop Connection on its default settings (Default.rdp),
 * which every connection shares, and says how to keep the changes there.
 */
export function useEditDefaults(): () => void {
  const { t } = useTranslation();
  const notify = useNotify();
  return () => {
    DiagService.EditDefaults().then(
      () => notify.success(t("diag.editDefaultsOpened")),
      (e: unknown) => notify.error(e, t("diag.editDefaultsFailed")),
    );
  };
}
