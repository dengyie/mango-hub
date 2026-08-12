import { resolveFlagCode } from "@/utils/flag";
import type { LiveData } from "@/types/LiveData";

type RegionStatus = "online" | "offline" | "partial";

export type EarthNodeInput = {
  uuid: string;
  name: string;
  region: string;
};

export type EarthUserGeo = {
  lat?: number;
  lng?: number;
  city?: string;
};

export type EarthPointData = {
  code: string;
  lat: number;
  lng: number;
  servers: string[];
  type: "server" | "user";
  size: number;
  online: number;
  offline: number;
  status: RegionStatus;
};

export type EarthArcData = {
  startLat: number;
  startLng: number;
  endLat: number;
  endLng: number;
};

type BuildEarthGlobeDataInput = {
  nodes: EarthNodeInput[];
  liveData?: LiveData | null;
  userGeo?: EarthUserGeo;
  demoMode?: boolean;
};

type EarthRegionGroup = {
  code: string;
  servers: string[];
  online: number;
  offline: number;
};

export const DEFAULT_EARTH_USER_GEO = {
  lat: 35.8617,
  lng: 104.1954,
  city: "Your Location",
} as const;

export const EARTH_COORD_MAP: Record<string, [number, number]> = {
  CN: [35.8617, 104.1954],
  HK: [22.3193, 114.1694],
  TW: [23.6978, 120.9605],
  JP: [36.2048, 138.2529],
  KR: [35.9078, 127.7669],
  SG: [1.3521, 103.8198],
  TH: [15.87, 100.9925],
  VN: [14.0583, 108.2772],
  MY: [4.2105, 101.9758],
  ID: [-0.7893, 113.9213],
  PH: [12.8797, 121.774],
  IN: [20.5937, 78.9629],
  PK: [30.3753, 69.3451],
  BD: [23.685, 90.3563],
  MM: [21.9162, 95.956],
  KH: [12.5657, 104.991],
  LA: [19.8563, 102.4955],
  NP: [28.3949, 84.124],
  LK: [7.8731, 80.7718],
  MN: [46.8625, 103.8467],
  KZ: [48.0196, 66.9237],
  UZ: [41.3775, 64.5853],
  AZ: [40.1431, 47.5769],
  GE: [42.3154, 43.3569],
  AM: [40.0691, 45.0382],
  KW: [29.3117, 47.4818],
  QA: [25.3548, 51.1839],
  BH: [26.0667, 50.5577],
  OM: [21.4735, 55.9754],
  JO: [30.5852, 36.2384],
  LB: [33.8547, 35.8623],
  IQ: [33.2232, 43.6793],
  IR: [32.4279, 53.688],
  AF: [33.9391, 67.71],
  AE: [23.4241, 53.8478],
  SA: [23.8859, 45.0792],
  IL: [31.0461, 34.8516],
  YE: [15.5527, 48.5164],
  TR: [38.9637, 35.2433],
  GB: [55.3781, -3.436],
  DE: [51.1657, 10.4515],
  FR: [46.2276, 2.2137],
  IT: [41.8719, 12.5674],
  ES: [40.4637, -3.7492],
  NL: [52.1326, 5.2913],
  BE: [50.5039, 4.4699],
  CH: [46.8182, 8.2275],
  AT: [47.5162, 14.5501],
  SE: [60.1282, 18.6435],
  NO: [60.472, 8.4689],
  FI: [61.9241, 25.7482],
  DK: [56.2639, 9.5018],
  IE: [53.4129, -8.2439],
  PT: [39.3999, -8.2245],
  GR: [39.0742, 21.8243],
  PL: [51.9194, 19.1451],
  CZ: [49.8175, 15.473],
  RO: [45.9432, 24.9668],
  HU: [47.1625, 19.5033],
  UA: [48.3794, 31.1656],
  RU: [61.524, 105.3188],
  SK: [48.669, 19.699],
  BG: [42.7339, 25.4858],
  HR: [45.1, 15.2],
  RS: [44.0165, 21.0059],
  SI: [46.1512, 14.9955],
  LT: [55.1694, 23.8813],
  LV: [56.8796, 24.6032],
  EE: [58.5953, 25.0136],
  BY: [53.7098, 27.9534],
  MD: [47.4116, 28.3699],
  IS: [64.9631, -19.0208],
  LU: [49.8153, 6.1296],
  MT: [35.9375, 14.3754],
  CY: [35.1264, 33.4299],
  AL: [41.1533, 20.1683],
  MK: [41.5124, 21.7465],
  BA: [43.9159, 17.6791],
  ME: [42.7087, 19.3744],
  US: [37.0902, -95.7129],
  CA: [56.1304, -106.3468],
  MX: [23.6345, -102.5528],
  GT: [15.7835, -90.2308],
  CU: [21.5218, -77.7812],
  HT: [18.9712, -72.2852],
  DO: [18.7357, -70.1627],
  JM: [18.1096, -77.2975],
  PR: [18.2208, -66.5901],
  PA: [8.538, -80.7821],
  CR: [9.7489, -83.7534],
  NI: [12.8654, -85.2072],
  HN: [15.2, -86.2419],
  SV: [13.7942, -88.8965],
  BZ: [17.1899, -88.4976],
  BR: [-14.235, -51.9253],
  AR: [-38.4161, -63.6167],
  CL: [-35.6751, -71.543],
  CO: [4.5709, -74.2973],
  PE: [-9.19, -75.0152],
  VE: [6.4238, -66.5897],
  EC: [-1.8312, -78.1834],
  BO: [-16.2902, -63.5887],
  PY: [-23.4425, -58.4438],
  UY: [-32.5228, -55.7658],
  GY: [4.8604, -58.9302],
  SR: [3.9193, -56.0278],
  AU: [-25.2744, 133.7751],
  NZ: [-40.9006, 174.886],
  FJ: [-17.7134, 178.065],
  PG: [-6.315, 143.9555],
  NC: [-20.9043, 165.618],
  ZA: [-30.5595, 22.9375],
  EG: [26.8206, 30.8025],
  NG: [9.082, 8.6753],
  KE: [-0.0236, 37.9062],
  MA: [31.7917, -7.0926],
  DZ: [28.0339, 1.6596],
  TN: [33.8869, 9.5375],
  GH: [7.9465, -1.0232],
  ET: [9.145, 40.4897],
  TZ: [-6.369, 34.8888],
  UG: [1.3733, 32.2903],
  CI: [7.54, -5.5471],
  SN: [14.4974, -14.4524],
  CM: [7.3697, 12.3547],
  AO: [-11.2027, 17.8739],
  MZ: [-18.6657, 35.5296],
  MG: [-18.7669, 46.8691],
  ZW: [-19.0154, 29.1549],
  BW: [-22.3285, 24.6849],
  NA: [-22.9576, 18.4904],
  RW: [-1.9403, 29.8739],
  MU: [-20.3484, 57.5522],
  LY: [26.3351, 17.2283],
  SD: [12.8628, 30.2176],
};

function getRegionStatus(online: number, offline: number): RegionStatus {
  if (online === 0) return "offline";
  if (offline === 0) return "online";
  return "partial";
}

function getValidNumber(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

export function resolveEarthCountryCode(region: string): string | null {
  const code = resolveFlagCode(region);
  return EARTH_COORD_MAP[code] ? code : null;
}

export function buildEarthGlobeData({
  nodes,
  liveData,
  userGeo,
  demoMode = false,
}: BuildEarthGlobeDataInput) {
  const onlineSet = new Set(liveData?.online ?? []);
  const hasLiveStatus = !!liveData;
  const sourceNodes: EarthNodeInput[] = demoMode
    ? Object.keys(EARTH_COORD_MAP).map((code) => ({
        uuid: `demo-${code}`,
        name: code,
        region: code,
      }))
    : nodes;
  const groups = new Map<string, EarthRegionGroup>();
  let totalCount = 0;

  sourceNodes.forEach((node) => {
    const code = resolveEarthCountryCode(node.region);
    if (!code) return;

    const existing = groups.get(code) ?? {
      code,
      servers: [],
      online: 0,
      offline: 0,
    };
    const isOnline = demoMode || !hasLiveStatus || onlineSet.has(node.uuid);

    existing.servers.push(node.name || node.uuid || code);
    existing.online += isOnline ? 1 : 0;
    existing.offline += isOnline ? 0 : 1;
    groups.set(code, existing);
    totalCount += 1;
  });

  const userLat = getValidNumber(userGeo?.lat, DEFAULT_EARTH_USER_GEO.lat);
  const userLng = getValidNumber(userGeo?.lng, DEFAULT_EARTH_USER_GEO.lng);
  const userCity = userGeo?.city?.trim() || DEFAULT_EARTH_USER_GEO.city;

  const serverPoints: EarthPointData[] = Array.from(groups.values())
    .sort((left, right) => right.servers.length - left.servers.length || left.code.localeCompare(right.code))
    .map((group) => {
      const [lat, lng] = EARTH_COORD_MAP[group.code];
      return {
        code: group.code,
        lat,
        lng,
        servers: group.servers,
        type: "server" as const,
        size: Math.min(Math.max(group.servers.length * 0.15, 0.5), 1.5),
        online: group.online,
        offline: group.offline,
        status: getRegionStatus(group.online, group.offline),
      };
    });

  const userPoint: EarthPointData = {
    code: "USER",
    lat: userLat,
    lng: userLng,
    servers: [userCity],
    type: "user",
    size: 2,
    online: 1,
    offline: 0,
    status: "online",
  };

  const arcsData: EarthArcData[] = serverPoints.map((point) => ({
    startLat: point.lat,
    startLng: point.lng,
    endLat: userLat,
    endLng: userLng,
  }));

  return {
    pointsData: [...serverPoints, userPoint],
    arcsData,
    totalCount,
  };
}
