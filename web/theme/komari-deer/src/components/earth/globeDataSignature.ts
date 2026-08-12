import type { EarthArcData, EarthPointData } from "./earthData";

function formatCoord(value: number) {
  return Number.isFinite(value) ? value.toFixed(4) : "0.0000";
}

export function getEarthStructuralSignature(
  pointsData: EarthPointData[],
  arcsData: EarthArcData[]
) {
  const pointSignature = pointsData
    .map((point) =>
      [
        point.type,
        point.code,
        formatCoord(point.lat),
        formatCoord(point.lng),
        point.servers.join("\u001f"),
      ].join("\u001e")
    )
    .sort()
    .join("\u001d");

  const arcSignature = arcsData
    .map((arc) =>
      [
        formatCoord(arc.startLat),
        formatCoord(arc.startLng),
        formatCoord(arc.endLat),
        formatCoord(arc.endLng),
      ].join("\u001e")
    )
    .sort()
    .join("\u001d");

  return `${pointSignature}\u001c${arcSignature}`;
}

export function getEarthStatusSignature(pointsData: EarthPointData[]) {
  return pointsData
    .map((point) =>
      [
        point.type,
        point.code,
        point.status,
        String(point.online),
        String(point.offline),
      ].join("\u001e")
    )
    .sort()
    .join("\u001d");
}

export function hasEarthStructuralChange(
  previousSignature: string,
  pointsData: EarthPointData[],
  arcsData: EarthArcData[]
) {
  return getEarthStructuralSignature(pointsData, arcsData) !== previousSignature;
}
