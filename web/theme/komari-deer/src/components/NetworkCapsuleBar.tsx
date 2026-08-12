"use client";

import { ArrowUp, Globe2 } from "lucide-react";
import { type CSSProperties, useCallback, useEffect, useRef, useState } from "react";

const SHOW_DELAY = 1500;
const STAY_TIME = 5000;
const REQUEST_TIMEOUT = 6000;
const NETWORK_CACHE_KEY = "deer-network-capsule-info";
const NETWORK_APIS = [
  "https://get.geojs.io/v1/ip/geo.json",
  "https://api.ip.sb/geoip",
  "https://ipwhois.app/json/",
  "https://freeipapi.com/api/json",
  "https://ipapi.co/json/",
];

type NetworkInfo = {
  ip: string;
  city: string;
  org: string;
};

function escapeHTML(value: string) {
  return String(value).replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  })[char] as string);
}

function normalizeNetworkData(data: any): NetworkInfo | null {
  if (!data || data.success === false) return null;

  const ip = data.ip || data.ipAddress;
  if (!ip) return null;

  return {
    ip,
    city:
      data.city ||
      data.cityName ||
      data.region ||
      data.regionName ||
      data.country_name ||
      data.countryName ||
      data.country ||
      "Unknown",
    org:
      data.org ||
      data.organization ||
      data.organization_name ||
      data.isp ||
      data.connection?.org ||
      data.connection?.isp ||
      data.asn_organization ||
      "",
  };
}

function formatNetworkInfo(networkInfo: NetworkInfo | null, loading: boolean) {
  if (networkInfo) {
    return [
      '<span class="deer-network-capsule-prefix">Your IP:</span> ',
      escapeHTML(networkInfo.ip),
      '<span class="deer-network-capsule-divider"></span>',
      "<span>",
      escapeHTML(networkInfo.city),
      "</span>",
      '<span class="deer-network-capsule-isp">&nbsp;',
      escapeHTML(networkInfo.org),
      "</span>",
    ].join("");
  }

  return loading ? "Fetching network info..." : "Network info unavailable";
}

async function fetchNetworkData(url: string) {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT);

  try {
    const response = await fetch(url, {
      signal: controller.signal,
      cache: "no-store",
    });
    if (!response.ok) throw new Error("IP API failed");

    const data = normalizeNetworkData(await response.json());
    if (!data) throw new Error("Invalid IP API response");
    return data;
  } finally {
    window.clearTimeout(timeout);
  }
}

function readCachedNetworkInfo() {
  try {
    const cached = window.sessionStorage.getItem(NETWORK_CACHE_KEY);
    return cached ? JSON.parse(cached) as NetworkInfo : null;
  } catch {
    return null;
  }
}

function writeCachedNetworkInfo(networkInfo: NetworkInfo) {
  try {
    window.sessionStorage.setItem(NETWORK_CACHE_KEY, JSON.stringify(networkInfo));
  } catch {}
}

export default function NetworkCapsuleBar() {
  const [networkInfo, setNetworkInfo] = useState<NetworkInfo | null>(null);
  const [networkLoading, setNetworkLoading] = useState(true);
  const [topVisible, setTopVisible] = useState(false);
  const lastScrollTopRef = useRef(0);
  const capsuleStyle = {
    "--deer-network-show-delay": `${SHOW_DELAY}ms`,
    "--deer-network-cycle-time": `${STAY_TIME + 1500}ms`,
  } as CSSProperties;

  const scrollToTop = useCallback(() => {
    window.scrollTo({ top: 0, behavior: "smooth" });
    document.documentElement.scrollTo?.({ top: 0, behavior: "smooth" });
    document.body.scrollTo?.({ top: 0, behavior: "smooth" });

    Array.from(document.querySelectorAll<HTMLElement>("main, div, section"))
      .filter((element) => {
        if (element === document.body || element === document.documentElement) return false;
        return element.scrollTop > 0;
      })
      .forEach((element) => {
        if (element.scrollTop <= 0) return;
        try {
          element.scrollTo({ top: 0, behavior: "smooth" });
        } catch {
          element.scrollTop = 0;
        }
      });
  }, []);

  useEffect(() => {
    let mounted = true;

    async function loadNetworkInfo() {
      const cached = readCachedNetworkInfo();
      if (cached) {
        if (mounted) {
          setNetworkInfo(cached);
          setNetworkLoading(false);
        }
        return;
      }

      for (const url of NETWORK_APIS) {
        try {
          const data = await fetchNetworkData(url);
          writeCachedNetworkInfo(data);
          if (mounted) {
            setNetworkInfo(data);
            setNetworkLoading(false);
          }
          return;
        } catch {}
      }

      if (mounted) setNetworkLoading(false);
    }

    void loadNetworkInfo();

    return () => {
      mounted = false;
    };
  }, []);

  useEffect(() => {
    let scrollTicking = false;

    const getScrollTop = (event?: Event) => {
      const eventTargetScroll =
        event?.target instanceof HTMLElement ? event.target.scrollTop || 0 : 0;

      return Math.max(
        window.scrollY || 0,
        document.documentElement.scrollTop || 0,
        document.body.scrollTop || 0,
        eventTargetScroll,
      );
    };

    const update = () => {
      const nextVisible = lastScrollTopRef.current > 200;
      setTopVisible((current) => current === nextVisible ? current : nextVisible);
      scrollTicking = false;
    };

    const scheduleUpdate = (event?: Event) => {
      lastScrollTopRef.current = getScrollTop(event);
      if (scrollTicking) return;
      scrollTicking = true;
      window.requestAnimationFrame(update);
    };

    window.addEventListener("scroll", scheduleUpdate, { capture: true, passive: true });
    window.addEventListener("resize", scheduleUpdate);
    scheduleUpdate();

    return () => {
      window.removeEventListener("scroll", scheduleUpdate, true);
      window.removeEventListener("resize", scheduleUpdate);
    };
  }, []);

  return (
    <>
      <div
        className="deer-network-capsule"
        style={capsuleStyle}
        role="status"
        aria-live="polite"
      >
        <Globe2 className="deer-network-capsule-icon" aria-hidden="true" />
        <span
          className="deer-network-capsule-text"
          dangerouslySetInnerHTML={{ __html: formatNetworkInfo(networkInfo, networkLoading) }}
        />
      </div>
      <button
        className={`deer-back-top${topVisible ? " is-visible" : ""}`}
        type="button"
        aria-label="Back to top"
        title="Back to top"
        onClick={scrollToTop}
      >
        <ArrowUp aria-hidden="true" />
      </button>
    </>
  );
}
