"use client";

import { useEffect, useState } from "react";

export const EARTH_GLOBE_OPEN_EVENT = "deer-earth-open-change";

export function getEarthGlobeOpenState() {
  return typeof document !== "undefined" && document.body.dataset.deerEarthOpen === "true";
}

export function setEarthGlobeOpenState(open: boolean) {
  if (typeof document === "undefined" || typeof window === "undefined") return;

  if (open) {
    document.body.dataset.deerEarthOpen = "true";
  } else {
    delete document.body.dataset.deerEarthOpen;
  }

  window.dispatchEvent(
    new CustomEvent(EARTH_GLOBE_OPEN_EVENT, {
      detail: { open },
    }),
  );
}

export function useEarthGlobeOpen() {
  const [isOpen, setIsOpen] = useState(false);

  useEffect(() => {
    const sync = (event?: Event) => {
      if (event instanceof CustomEvent && typeof event.detail?.open === "boolean") {
        setIsOpen(event.detail.open);
        return;
      }

      setIsOpen(getEarthGlobeOpenState());
    };

    sync();
    window.addEventListener(EARTH_GLOBE_OPEN_EVENT, sync);
    return () => window.removeEventListener(EARTH_GLOBE_OPEN_EVENT, sync);
  }, []);

  return isOpen;
}
