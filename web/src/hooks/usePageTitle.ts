import { useEffect } from "react";

const APP_NAME = "Back-Orbit";

/**
 * usePageTitle keeps the browser tab and history entries meaningful. Without
 * it every route shared the single title "Back-Orbit", which made open tabs
 * and browser history indistinguishable.
 */
export function usePageTitle(title?: string) {
  useEffect(() => {
    document.title = title ? `${title} · ${APP_NAME}` : APP_NAME;
    return () => {
      document.title = APP_NAME;
    };
  }, [title]);
}
