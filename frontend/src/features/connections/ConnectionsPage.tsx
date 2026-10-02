import { DesktopArrowRight24Regular } from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { EmptyState, Page } from "../../components/Page";

export function ConnectionsPage() {
  const { t } = useTranslation();
  return (
    <Page title={t("connections.title")}>
      <EmptyState
        icon={<DesktopArrowRight24Regular />}
        title={t("connections.emptyTitle")}
        body={t("connections.emptyBody")}
      />
    </Page>
  );
}
