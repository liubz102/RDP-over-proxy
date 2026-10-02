import { Globe24Regular } from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { EmptyState, Page } from "../../components/Page";

export function ProxiesPage() {
  const { t } = useTranslation();
  return (
    <Page title={t("proxies.title")}>
      <EmptyState icon={<Globe24Regular />} title={t("proxies.emptyTitle")} body={t("proxies.emptyBody")} />
    </Page>
  );
}
