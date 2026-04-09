import { getRegion } from './mapApi';

export interface RegionUiDefaults {
  code: string;
  mapCenter: [number, number];
  mapZoom: number;
  originPlaceholder: string;
  destinationPlaceholder: string;
}

const DEFAULTS_BY_REGION: Record<string, RegionUiDefaults> = {
  eu: {
    code: 'eu',
    mapCenter: [50.1109, 8.6821],
    mapZoom: 5,
    originPlaceholder: 'Search origin (e.g. Frankfurt, Paris, Amsterdam...)',
    destinationPlaceholder: 'Search destination (e.g. Berlin, Milan, Brussels...)',
  },
  us: {
    code: 'us',
    mapCenter: [39.8283, -98.5795],
    mapZoom: 4,
    originPlaceholder: 'Search origin (e.g. New York, Chicago, Dallas...)',
    destinationPlaceholder: 'Search destination (e.g. Los Angeles, Seattle, Miami...)',
  },
  apac: {
    code: 'apac',
    mapCenter: [1.3521, 103.8198],
    mapZoom: 4,
    originPlaceholder: 'Search origin (e.g. Singapore, Tokyo, Sydney...)',
    destinationPlaceholder: 'Search destination (e.g. Bangkok, Seoul, Melbourne...)',
  },
  local: {
    code: 'local',
    mapCenter: [53.3498, -6.2603],
    mapZoom: 11,
    originPlaceholder: 'Search origin (e.g. City Centre, North Gate...)',
    destinationPlaceholder: 'Search destination (e.g. Airport, South Terminal...)',
  },
  unknown: {
    code: 'unknown',
    mapCenter: [20.0, 0.0],
    mapZoom: 2,
    originPlaceholder: 'Search origin location',
    destinationPlaceholder: 'Search destination location',
  },
};

export const DEFAULT_REGION_UI_DEFAULTS = DEFAULTS_BY_REGION.unknown;

export function normalizeRegionCode(rawRegion: string | null | undefined): string {
  const trimmed = (rawRegion ?? '').trim().toLowerCase();
  if (!trimmed) return 'unknown';

  if (trimmed === 'eu' || trimmed === 'europe') return 'eu';
  if (trimmed === 'us' || trimmed === 'usa' || trimmed === 'na' || trimmed === 'north-america') return 'us';
  if (trimmed === 'apac' || trimmed === 'asia' || trimmed === 'asia-pacific') return 'apac';
  if (trimmed === 'local' || trimmed === 'dev') return 'local';

  return 'unknown';
}

export function getRegionUiDefaultsFromCode(regionCode: string): RegionUiDefaults {
  return DEFAULTS_BY_REGION[normalizeRegionCode(regionCode)] ?? DEFAULT_REGION_UI_DEFAULTS;
}

export async function getRegionUiDefaults(): Promise<RegionUiDefaults> {
  const region = await getRegion();
  return getRegionUiDefaultsFromCode(region);
}
