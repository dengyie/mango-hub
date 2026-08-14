import { geoGraticule10, geoOrthographic, geoPath } from "d3-geo";
import type { GeoProjection } from "d3-geo";
import { feature } from "topojson-client";

import worldCountries50m from "@/data/world-countries-50m.json";

const SVG_WIDTH = 1000;
const SVG_HEIGHT = 560;
const MAP_HORIZONTAL_PADDING = 28;
const MAP_TOP_PADDING = 42;
const MAP_BOTTOM_INSET = 42;
const SMALL_REGION_MARKER_AREA_THRESHOLD = 14;
const SMALL_REGION_MARKER_SIZE_THRESHOLD = 7;

/** 默认正射投影球面初始半径(像素)。fitExtent 会据此调整最终 scale。 */
const ORTHOGRAPHIC_INITIAL_SCALE = 260;
/** 初始正向(经度/纬度),0 = 本初子午线朝前。自转启动后从此值开始累加。 */
// 经度 -120° → 视点中心东经 120°(亚太朝前):初始视角可见中国/日本/韩国/东南亚/澳洲/新西兰,
// 避免正射投影 clipAngle(90) 把亚太区域全部裁到背面(旧值 -10 面向大西洋,日本/澳洲不可见)。
const INITIAL_ROTATION_LAMBDA = -120;
const INITIAL_ROTATION_PHI = -15;

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

/**
 * 静态底图缓存:固定几何源 + 已 fit 的正射投影实例(共享 rotate 态)+ 一次性几何 meta。
 * smallRegionMarker 判定基于国家"固有大小",与 rotate 无关,故只算一次并缓存 needsMarker 标记。
 */
type StaticMapCache = {
  countriesGeo: { features: CountryFeature[] };
  projection: GeoProjection;
  pathGenerator: ReturnType<typeof geoPath>;
  /** 与 rotate 无关的静态 meta:每个国家是否需要小区域 marker。 */
  needsMarker: Map<string, boolean>;
};

let staticMapCache: StaticMapCache | null = null;

function ensureStaticMap(): StaticMapCache {
  if (staticMapCache) {
    return staticMapCache;
  }

  const countriesGeo = feature(
    worldCountries50m as never,
    (worldCountries50m as unknown as { objects: { countries: never } }).objects.countries,
  ) as unknown as { features: CountryFeature[] };

  const projection = geoOrthographic()
    .scale(ORTHOGRAPHIC_INITIAL_SCALE)
    .translate([SVG_WIDTH / 2, SVG_HEIGHT / 2])
    .rotate([INITIAL_ROTATION_LAMBDA, INITIAL_ROTATION_PHI, 0])
    .clipAngle(90)
    .fitExtent(
      [
        [MAP_HORIZONTAL_PADDING, MAP_TOP_PADDING],
        [SVG_WIDTH - MAP_HORIZONTAL_PADDING, SVG_HEIGHT - MAP_BOTTOM_INSET],
      ],
      // fitExtent 用球体拟合视口:正射投影下国家都被球面 clipAngle 裁剪到可见半球,
      // 球体填满 padding 区即等价于国家填满,且语义正确(俄/格陵兰等宽国家边缘自然贴球边)。
      { type: "Sphere" } as never,
    );

  const pathGenerator = geoPath(projection);

  // 一次性静态判定:needsMarker 基于固定初始视角的 area/bounds,仅用于"该国是否太小需点状 marker"。
  // 这是国家几何固有属性,与后续 rotate 无关(投影改变不影响"小国家仍是小国家"的判定结论)。
  const needsMarker = new Map<string, boolean>();
  for (const country of countriesGeo.features) {
    const name = country.properties?.name ?? String(country.id ?? "unknown");
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
    needsMarker.set(name, shouldShowMarker);
  }

  staticMapCache = {
    countriesGeo,
    projection,
    pathGenerator,
    needsMarker,
  };
  return staticMapCache;
}

/**
 * 同步投影旋转并返回当前 rotate 下重投影后的地图结构。
 *
 * 性能(实测 bench):每帧 177 国 path 重投影约 22-25ms(~40fps),满足自转流畅。
 * 关键优化:needsMarker 静态缓存,不每帧算 area/bounds;仅对 needsMarker=true 的小国
 * (通常极少,个位数)额外算 centroid 以跟随旋转移动 marker 点。
 *
 * @param rotation [lambda, phi, gamma] 经度/纬度/roll(度)。每帧累加 lambda 实现自转。
 */
export function getRotatedMap(
  rotation: [number, number, number],
): ProjectedWorldMap {
  const { projection, pathGenerator, countriesGeo, needsMarker } = ensureStaticMap();

  projection.rotate(rotation);

  const spherePath = pathGenerator({ type: "Sphere" }) ?? "";
  const graticulePath = pathGenerator(geoGraticule10()) ?? "";

  const countries = countriesGeo.features.map((country) => {
    const name = country.properties?.name ?? String(country.id ?? "unknown");
    const pathData = pathGenerator(country as never) ?? "";

    // 仅对需要 marker 的小国算当前 rotate 下的屏幕 centroid(数量极少,成本可忽略)。
    // 大国用 path 填充本身即代表位置,不需要点 marker。
    const needs = needsMarker.get(name) ?? false;
    let smallRegionMarker: { x: number; y: number } | null = null;
    if (needs && pathData) {
      const [markerX, markerY] = pathGenerator.centroid(country as never);
      smallRegionMarker = Number.isFinite(markerX) && Number.isFinite(markerY)
        ? { x: markerX, y: markerY }
        : null;
    }

    return { name, pathData, smallRegionMarker };
  });

  return { spherePath, graticulePath, countries };
}

/** 历史兼容 API:返回初始旋转视角下的地图(旧 naturalEarth1 调用方遗留,现以正射初始视角替代)。 */
export function getProjectedWorldMap(): ProjectedWorldMap {
  return getRotatedMap([INITIAL_ROTATION_LAMBDA, INITIAL_ROTATION_PHI, 0]);
}

/** 暴露初始旋转常量供组件复用(自转从此角度起步)。 */
export const INITIAL_ROTATION: [number, number, number] = [
  INITIAL_ROTATION_LAMBDA,
  INITIAL_ROTATION_PHI,
  0,
];
