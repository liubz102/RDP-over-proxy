import { useEffect, useState } from "react";
import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  MessageBar,
  MessageBarBody,
  Spinner,
} from "@fluentui/react-components";
import { useTranslation } from "react-i18next";
import { errorOf, SessionService, type CheckView, type ErrorView, type ProfileView } from "../../api/backend";
import { ErrorBar } from "../../components/Feedback";

type State = { kind: "running" } | { kind: "passed"; result: CheckView } | { kind: "failed"; error: ErrorView };

/**
 * Checks that the profile's computer answers as a Remote Desktop server
 * through its proxy, without starting Remote Desktop. The check waits as
 * long as the route takes; closing the dialog cancels it.
 */
export function CheckDialog({ view, onClose }: { view: ProfileView; onClose: () => void }) {
  const { t } = useTranslation();
  const [state, setState] = useState<State>({ kind: "running" });

  useEffect(() => {
    let done = false;
    const call = SessionService.CheckRoute(view.profile.id);
    call.then(
      (result) => {
        done = true;
        setState({ kind: "passed", result });
      },
      (e: unknown) => {
        if (done) return;
        done = true;
        setState({ kind: "failed", error: errorOf(e) });
      },
    );
    return () => {
      // Closing the dialog (or StrictMode's trial unmount) stops the check.
      if (!done) {
        done = true;
        call.cancel();
      }
    };
  }, [view.profile.id]);

  return (
    <Dialog open onOpenChange={(_, d) => !d.open && onClose()}>
      <DialogSurface>
        <DialogBody>
          <DialogTitle>{t("connections.check.title", { name: view.profile.name })}</DialogTitle>
          <DialogContent>
            {state.kind === "running" && <Spinner labelPosition="after" label={t("connections.check.running")} />}
            {state.kind === "passed" && (
              <MessageBar intent="success" layout="multiline">
                <MessageBarBody>
                  {state.result.negotiationFailure
                    ? t("connections.check.passedNegotiation", {
                        ms: state.result.elapsedMs,
                        reason: state.result.negotiationFailure,
                      })
                    : t("connections.check.passed", { ms: state.result.elapsedMs, protocol: state.result.protocol })}
                </MessageBarBody>
              </MessageBar>
            )}
            {state.kind === "failed" && <ErrorBar error={state.error} title={t("connections.check.failed")} />}
          </DialogContent>
          <DialogActions>
            <Button appearance={state.kind === "running" ? "secondary" : "primary"} onClick={onClose}>
              {state.kind === "running" ? t("common.cancel") : t("common.close")}
            </Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}
