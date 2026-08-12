"use client";

import { useEffect, useRef, useState } from "react";
import { DEFAULT_EARTH_USER_GEO } from "./earthData";

export type UserGeoInfo = {
  lat: number;
  lng: number;
  city: string;
  country: string;
  region: string;
  ip: string;
  isp: string;
};

const defaultGeo: UserGeoInfo = {
  ...DEFAULT_EARTH_USER_GEO,
  country: "",
  region: "",
  ip: "",
  isp: "",
};

type GeoStrategy = {
  url: string;
  check: (input: any) => boolean;
  map: (input: any) => Partial<UserGeoInfo>;
};

let cachedGeo: UserGeoInfo | null = null;
let fetchPromise: Promise<UserGeoInfo> | null = null;

const geoStrategies: GeoStrategy[] = [
  {
    url: "https://api.ip.sb/geoip",
    check: (data) => "country" in data,
    map: (data) => ({
      country: data.country || "",
      region: data.region || "",
      city: data.city || "",
      isp: data.asn_organization || "",
      ip: data.ip || "",
      lat: Number(data.latitude),
      lng: Number(data.longitude),
    }),
  },
  {
    url: "https://ipwho.is",
    check: (data) => data.success === true,
    map: (data) => ({
      country: data.country || "",
      region: data.region || "",
      city: data.city || "",
      isp: data.connection?.isp || "",
      ip: data.ip || "",
      lat: Number(data.latitude),
      lng: Number(data.longitude),
    }),
  },
  {
    url: "https://api.ipapi.is",
    check: (data) => "location" in data && "country" in data.location,
    map: (data) => ({
      country: data.location?.country || "",
      region: data.location?.state || "",
      city: data.location?.city || "",
      isp: data.company?.name || data.datacenter?.datacenter || data.abuse?.name || "",
      ip: data.ip || "",
      lat: Number(data.location?.latitude),
      lng: Number(data.location?.longitude),
    }),
  },
];

function hasUsableCoordinate(value: Partial<UserGeoInfo>) {
  return Number.isFinite(value.lat) && Number.isFinite(value.lng);
}

async function fetchGeoInfo(): Promise<UserGeoInfo> {
  if (cachedGeo) return cachedGeo;
  if (fetchPromise) return fetchPromise;

  fetchPromise = (async () => {
    const result = { ...defaultGeo };

    for (const strategy of geoStrategies) {
      try {
        const response = await fetch(strategy.url, {
          signal: AbortSignal.timeout(5000),
        });

        if (!response.ok) continue;

        const data = await response.json();
        if (!strategy.check(data)) continue;

        const mapped = strategy.map(data);
        result.country = mapped.country || result.country;
        result.region = mapped.region || result.region;
        result.city = mapped.city || result.city;
        result.isp = mapped.isp || result.isp;
        result.ip = mapped.ip || result.ip;

        if (hasUsableCoordinate(mapped)) {
          result.lat = mapped.lat as number;
          result.lng = mapped.lng as number;
        }

        if (result.ip) break;
      } catch (error) {
        console.warn(`Failed to fetch geo info from ${strategy.url}:`, error);
      }
    }

    cachedGeo = result;
    return result;
  })();

  return fetchPromise;
}

export function useUserGeo(enabled = true) {
  const [geo, setGeo] = useState<UserGeoInfo>(cachedGeo || defaultGeo);
  const [loading, setLoading] = useState(enabled && !cachedGeo);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;

    if (!enabled) {
      setLoading(false);
      return () => {
        mounted.current = false;
      };
    }

    if (cachedGeo) {
      setGeo(cachedGeo);
      setLoading(false);
      return undefined;
    }

    setLoading(true);
    fetchGeoInfo().then((result) => {
      if (!mounted.current) return;
      setGeo(result);
      setLoading(false);
    });

    return () => {
      mounted.current = false;
    };
  }, [enabled]);

  return { geo, loading };
}
