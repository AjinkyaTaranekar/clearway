export const LOCATION_COORDINATES: Record<string, { lat: number; lng: number }> = {
  'City Centre':         { lat: 53.3498, lng: -6.2603 },
  'North Gate':          { lat: 53.3764, lng: -6.2681 },
  'South Terminal':      { lat: 53.3201, lng: -6.2785 },
  'East Quay':           { lat: 53.3478, lng: -6.2317 },
  'Westside Depot':      { lat: 53.3503, lng: -6.3112 },
  'West Depot':          { lat: 53.3503, lng: -6.3112 },
  'Northfield':          { lat: 53.3892, lng: -6.2891 },
  'Riverside':           { lat: 53.3367, lng: -6.2734 },
  'Airport':             { lat: 53.4273, lng: -6.2437 },
  'Airport North':       { lat: 53.4350, lng: -6.2437 },
  'Industrial Park':     { lat: 53.3289, lng: -6.2156 },
  'Port Terminal':       { lat: 53.3456, lng: -6.2089 },
  'West Industrial':     { lat: 53.3567, lng: -6.3234 },
  'Central Market':      { lat: 53.3441, lng: -6.2675 },
  'University District': { lat: 53.3382, lng: -6.2571 },
  'North Suburb':        { lat: 53.3901, lng: -6.2456 },
};

export function getCoordinates(name: string): { lat: number; lng: number } {
  return LOCATION_COORDINATES[name] ?? { lat: 53.3498, lng: -6.2603 };
}

/** Reverse lookup: find closest city name for given lat/lng */
export function getLocationName(lat: number, lng: number): string {
  let best = '';
  let bestDist = Infinity;
  for (const [name, coords] of Object.entries(LOCATION_COORDINATES)) {
    const d = Math.abs(coords.lat - lat) + Math.abs(coords.lng - lng);
    if (d < bestDist) {
      bestDist = d;
      best = name;
    }
  }
  return best || `${lat.toFixed(4)},${lng.toFixed(4)}`;
}
