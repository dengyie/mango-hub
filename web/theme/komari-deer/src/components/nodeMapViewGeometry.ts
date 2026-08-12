import { geoGraticule10, geoNaturalEarth1, geoPath } from "d3-geo";
import { feature } from "topojson-client";

import worldCountries50m from "@/data/world-countries-50m.json";

const SVG_WIDTH = 1000;
const SVG_HEIGHT = 560;
const MAP_HORIZONTAL_PADDING = 28;
const MAP_TOP_PADDING = 42;
const MAP_BOTTOM_INSET = 42;
const SMALL_REGION_MARKER_AREA_THRESHOLD = 14;
const SMALL_REGION_MARKER_SIZE_THRESHOLD = 7;

type CountryFeature = {
  id?: string;
  properties?: {
    name?: string;
  };
};

export type NodeMapCountryGeometry = {
  name: string;
  pathData: string;
  smallRegionMarker: {
    x: number;
    y: number;
  } | null;
};

export type ProjectedWorldMap = {
  spherePath: string;
  graticulePath: string;
  countries: NodeMapCountryGeometry[];
};

let projectedWorldMapCache: ProjectedWorldMap | null = null;

export function getProjectedWorldMap(): ProjectedWorldMap {
  if (projectedWorldMapCache) {
    return projectedWorldMapCache;
  }

  const countriesGeo = feature(
    worldCountries50m as never,
    (worldCountries50m as unknown as { objects: { countries: never } }).objects.countries,
  ) as unknown as { features: CountryFeature[] };

  const projection = geoNaturalEarth1().fitExtent(
    [
      [MAP_HORIZONTAL_PADDING, MAP_TOP_PADDING],
      [SVG_WIDTH - MAP_HORIZONTAL_PADDING, SVG_HEIGHT - MAP_BOTTOM_INSET],
    ],
    countriesGeo as never,
  );

  const pathGenerator = geoPath(projection);
  const spherePath = pathGenerator({ type: "Sphere" }) ?? "";
  const graticulePath = pathGenerator(geoGraticule10()) ?? "";

  const countries = countriesGeo.features
    .map((country) => {
      const name = country.properties?.name ?? String(country.id ?? "unknown");
      const pathData = pathGenerator(country as never) ?? "";
      const bounds = pathGenerator.bounds(country as never);
      const width = bounds[1][0] - bounds[0][0];
      const height = bounds[1][1] - bounds[0][1];
      const area = pathGenerator.area(country as never);
      const [markerX, markerY] = pathGenerator.centroid(country as never);
      const shouldShowMarker =
        Number.isFinite(markerX) &&
        Number.isFinite(markerY) &&
        (area < SMALL_REGION_MARKER_AREA_THRESHOLD ||
          Math.max(width, height) < SMALL_REGION_MARKER_SIZE_THRESHOLD);

      return {
        name,
        pathData,
        smallRegionMarker: shouldShowMarker
          ? {
              x: markerX,
              y: markerY,
            }
          : null,
      };
    })
    .filter((country) => country.pathData);

  projectedWorldMapCache = {
    spherePath,
    graticulePath,
    countries,
  };

  return projectedWorldMapCache;
}
