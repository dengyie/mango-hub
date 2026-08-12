import { feature } from "topojson-client";

import worldCountries110m from "@/data/world-countries-110m.json";
import { EARTH_COORD_MAP } from "./earthData";

export type EarthCountryPolygon = {
  code: string;
  geometry: unknown;
  id?: string | number;
  synthetic?: boolean;
};

const ISO_NUMERIC_BY_CODE: Record<string, string> = {
  AF: "4",
  AL: "8",
  DZ: "12",
  AO: "24",
  AZ: "31",
  AR: "32",
  AU: "36",
  AT: "40",
  BH: "48",
  BD: "50",
  AM: "51",
  BE: "56",
  BO: "68",
  BA: "70",
  BW: "72",
  BR: "76",
  BZ: "84",
  BG: "100",
  MM: "104",
  BY: "112",
  KH: "116",
  CM: "120",
  CA: "124",
  CL: "152",
  CN: "156",
  TW: "158",
  CO: "170",
  CR: "188",
  HR: "191",
  CU: "192",
  CY: "196",
  CZ: "203",
  DK: "208",
  DO: "214",
  EC: "218",
  SV: "222",
  EE: "233",
  ET: "231",
  FJ: "242",
  FI: "246",
  FR: "250",
  GE: "268",
  DE: "276",
  GH: "288",
  GR: "300",
  GT: "320",
  GY: "328",
  HT: "332",
  HN: "340",
  HK: "344",
  HU: "348",
  IS: "352",
  IN: "356",
  ID: "360",
  IR: "364",
  IQ: "368",
  IE: "372",
  IL: "376",
  IT: "380",
  CI: "384",
  JM: "388",
  JP: "392",
  JO: "400",
  KZ: "398",
  KE: "404",
  KR: "410",
  KW: "414",
  LA: "418",
  LB: "422",
  LV: "428",
  LY: "434",
  LT: "440",
  LU: "442",
  MG: "450",
  MY: "458",
  MT: "470",
  MX: "484",
  MD: "498",
  ME: "499",
  MN: "496",
  MA: "504",
  MZ: "508",
  OM: "512",
  NA: "516",
  NP: "524",
  NL: "528",
  NC: "540",
  NZ: "554",
  NI: "558",
  NG: "566",
  NO: "578",
  PK: "586",
  PA: "591",
  PG: "598",
  PY: "600",
  PE: "604",
  PH: "608",
  PL: "616",
  PT: "620",
  PR: "630",
  QA: "634",
  RO: "642",
  RU: "643",
  RW: "646",
  SA: "682",
  SN: "686",
  RS: "688",
  SG: "702",
  SK: "703",
  VN: "704",
  SI: "705",
  ZA: "710",
  ZW: "716",
  ES: "724",
  SD: "729",
  SR: "740",
  SE: "752",
  CH: "756",
  TH: "764",
  TN: "788",
  TR: "792",
  UG: "800",
  UA: "804",
  MK: "807",
  EG: "818",
  GB: "826",
  TZ: "834",
  US: "840",
  UY: "858",
  UZ: "860",
  VE: "862",
  YE: "887",
};

type CountryFeature = {
  id?: string | number;
  geometry?: unknown;
};

type Position = [number, number];
type PolygonCoordinates = Position[][];
type EarthPolygonGeometry =
  | {
      type: "Polygon";
      coordinates: PolygonCoordinates;
    }
  | {
      type: "MultiPolygon";
      coordinates: PolygonCoordinates[];
    };

let featuresByIdCache: Map<string, CountryFeature> | null = null;
const polygonResultCache = new Map<string, EarthCountryPolygon[]>();

function normalizeCodes(codes: string[]) {
  return Array.from(
    new Set(codes.map((code) => code.trim().toUpperCase()).filter(Boolean)),
  ).sort();
}

function getCountryFeaturesById() {
  if (featuresByIdCache) {
    return featuresByIdCache;
  }

  const countriesGeo = feature(
    worldCountries110m as never,
    (worldCountries110m as unknown as { objects: { countries: never } }).objects.countries,
  ) as unknown as { features: CountryFeature[] };

  featuresByIdCache = new Map(
    countriesGeo.features
      .filter((country) => country.id !== undefined && country.geometry)
      .map((country) => [String(country.id), country]),
  );

  return featuresByIdCache;
}

function getCountryFeatureByNumericId(featuresById: Map<string, CountryFeature>, numericId?: string) {
  if (!numericId) return undefined;

  return featuresById.get(numericId) ?? featuresById.get(numericId.padStart(3, "0"));
}

function getRingArea(ring: Position[]) {
  let area = 0;

  for (let i = 0, previous = ring.length - 1; i < ring.length; previous = i, i += 1) {
    area += ring[previous][0] * ring[i][1] - ring[i][0] * ring[previous][1];
  }

  return area / 2;
}

function orientRing(ring: Position[], shouldBePositive: boolean) {
  const isPositive = getRingArea(ring) > 0;
  const nextRing = ring.map((position) => [position[0], position[1]] as Position);
  return isPositive === shouldBePositive ? nextRing : nextRing.reverse();
}

function rewindPolygonCoordinates(coordinates: PolygonCoordinates) {
  return coordinates.map((ring, index) => orientRing(ring, index !== 0));
}

function rewindGeometry(geometry: unknown): unknown {
  const typedGeometry = geometry as EarthPolygonGeometry;

  if (typedGeometry.type === "Polygon") {
    return {
      type: "Polygon",
      coordinates: rewindPolygonCoordinates(typedGeometry.coordinates),
    };
  }

  if (typedGeometry.type === "MultiPolygon") {
    return {
      type: "MultiPolygon",
      coordinates: typedGeometry.coordinates.map(rewindPolygonCoordinates),
    };
  }

  return geometry;
}

function createSmallRegionGeometry(code: string) {
  const coord = EARTH_COORD_MAP[code];
  if (!coord) return null;

  const [lat, lng] = coord;
  const radius = 1.1;
  const latRadians = (lat * Math.PI) / 180;
  const lngRadius = radius / Math.max(Math.cos(latRadians), 0.35);
  const coordinates: [number, number][] = [];

  for (let i = 0; i <= 18; i += 1) {
    const angle = (i / 18) * Math.PI * 2;
    coordinates.push([
      Number((lng + Math.cos(angle) * lngRadius).toFixed(4)),
      Number((lat + Math.sin(angle) * radius).toFixed(4)),
    ]);
  }

  return {
    type: "Polygon",
    coordinates: [coordinates],
  };
}

export function getEarthCountryPolygons(codes: string[]): EarthCountryPolygon[] {
  const normalizedCodes = normalizeCodes(codes);
  const cacheKey = normalizedCodes.join("|");
  const cached = polygonResultCache.get(cacheKey);

  if (cached) {
    return cached;
  }

  const featuresById = getCountryFeaturesById();
  const polygons: EarthCountryPolygon[] = [];

  normalizedCodes.forEach((code) => {
    const numericId = ISO_NUMERIC_BY_CODE[code];
    const country = getCountryFeatureByNumericId(featuresById, numericId);

    if (country?.geometry) {
      polygons.push({
        code,
        geometry: rewindGeometry(country.geometry),
        id: country.id,
      });
      return;
    }

    const smallRegionGeometry = createSmallRegionGeometry(code);
    if (!smallRegionGeometry) return;

    polygons.push({
      code,
      geometry: rewindGeometry(smallRegionGeometry),
      id: `small-${code}`,
      synthetic: true,
    });
  });

  polygonResultCache.set(cacheKey, polygons);
  return polygons;
}
