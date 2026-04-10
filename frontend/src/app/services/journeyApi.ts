import { GeoPoint, Journey, JourneyStatus, RouteSegment, TimelineEvent, VehicleType } from '../types';
import { authHeaders, getToken } from './auth';
import { getCoordinates, getLocationName } from './coordinates';

// Prefer relative URLs (nginx will proxy to the right backend). For local/dev
// you can set VITE_API_URL to point at a specific gateway or host.
const BASE_URL = import.meta.env.VITE_API_URL ?? '';

const JOURNEY_DETAIL_CACHE_TTL_MS = 60_000;
const JOURNEY_LIST_CACHE_TTL_MS = 30_000;
const SEGMENT_OCCUPANCY_CACHE_TTL_MS = 15_000;
const DRIVER_NAME_CACHE_TTL_MS = 5 * 60_000;

type CacheEntry<T> = {
  value: T;
  expiresAt: number;
};

type APIJourneyEvent = {
  event_id?: string;
  journey_id?: string;
  event_type?: string;
  actor_type?: string;
  actor_id?: string;
  note?: string;
  timestamp?: string;
};

const journeyDetailCache = new Map<string, CacheEntry<Journey>>();
const journeyListCache = new Map<string, CacheEntry<{ journeys: Journey[]; total: number }>>();
const adminJourneyListCache = new Map<string, CacheEntry<{ journeys: Journey[]; total: number }>>();
const driverNameCache = new Map<string, CacheEntry<string>>();
let occupancyCacheEntry: CacheEntry<Map<string, SegmentOccupancy>> | null = null;

function cacheScope(): string {
  const token = getToken();
  if (!token) return 'anon';
  // Scope caches per logged-in session to avoid cross-user reuse.
  return token.slice(-20);
}

function readCache<T>(cache: Map<string, CacheEntry<T>>, key: string): T | null {
  const entry = cache.get(key);
  if (!entry) return null;
  if (entry.expiresAt <= Date.now()) {
    cache.delete(key);
    return null;
  }
  return entry.value;
}

function writeCache<T>(cache: Map<string, CacheEntry<T>>, key: string, value: T, ttlMs: number): T {
  cache.set(key, {
    value,
    expiresAt: Date.now() + ttlMs,
  });
  return value;
}

function invalidateScopedListCaches(scope: string) {
  for (const key of journeyListCache.keys()) {
    if (key.startsWith(`${scope}|`)) {
      journeyListCache.delete(key);
    }
  }
  for (const key of adminJourneyListCache.keys()) {
    if (key.startsWith(`${scope}|`)) {
      adminJourneyListCache.delete(key);
    }
  }
}

function invalidateJourneyReadCaches(journeyID?: string) {
  const scope = cacheScope();
  if (journeyID) {
    journeyDetailCache.delete(`${scope}|journey:${journeyID}`);
  } else {
    for (const key of journeyDetailCache.keys()) {
      if (key.startsWith(`${scope}|`)) {
        journeyDetailCache.delete(key);
      }
    }
  }
  invalidateScopedListCaches(scope);
}

export function clearJourneyReadCache(): void {
  journeyDetailCache.clear();
  journeyListCache.clear();
  adminJourneyListCache.clear();
  driverNameCache.clear();
  occupancyCacheEntry = null;
}

function fallbackDriverName(driverID: string): string {
  const trimmed = (driverID ?? '').trim();
  if (!trimmed) return 'Driver';
  if (trimmed.length <= 8) return `Driver ${trimmed}`;
  return `Driver ${trimmed.slice(0, 8)}`;
}

async function resolveDriverNames(driverIDs: string[]): Promise<Map<string, string>> {
  const names = new Map<string, string>();
  const scope = cacheScope();
  const uniqueIDs = Array.from(new Set(driverIDs.filter((id) => id.trim() !== '')));

  const misses: string[] = [];
  uniqueIDs.forEach((driverID) => {
    const key = `${scope}|driver-name:${driverID}`;
    const cached = readCache(driverNameCache, key);
    if (cached) {
      names.set(driverID, cached);
      return;
    }
    misses.push(driverID);
  });

  if (misses.length === 0) {
    return names;
  }

  await Promise.all(misses.map(async (driverID) => {
    try {
      const user = await apiFetch<{ id: string; name?: string }>(`/api/v1/admin/auth/users/${encodeURIComponent(driverID)}`);
      const resolvedName = (user?.name ?? '').trim();
      if (resolvedName) {
        names.set(driverID, writeCache(driverNameCache, `${scope}|driver-name:${driverID}`, resolvedName, DRIVER_NAME_CACHE_TTL_MS));
      }
    } catch {
      // Keep fallback name if lookup is unavailable.
    }
  }));

  return names;
}

function generateIdempotencyKey(): string {
  const cryptoApi = globalThis.crypto;
  if (cryptoApi?.randomUUID) {
    return cryptoApi.randomUUID();
  }

  if (cryptoApi?.getRandomValues) {
    const bytes = new Uint8Array(16);
    cryptoApi.getRandomValues(bytes);

    // RFC 4122 v4 UUID bits
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;

    const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}`;
  }

  return `idemp-${Date.now().toString(16)}-${Math.random().toString(16).slice(2)}-${Math.random().toString(16).slice(2)}`;
}

// ---- Type mapping helpers ----

function mapStatus(apiStatus: string): JourneyStatus {
  return apiStatus.toLowerCase() as JourneyStatus;
}

function mapVehicleType(apiType: string): VehicleType {
  const map: Record<string, VehicleType> = {
    car: 'Car',
    van: 'Van',
    truck: 'HGV',
    motorcycle: 'Motorcycle',
  };
  return map[apiType.toLowerCase()] ?? 'Car';
}

function toApiVehicleType(frontendType: string): string {
  const map: Record<string, string> = {
    Car: 'car',
    Van: 'van',
    HGV: 'truck',
    Motorcycle: 'motorcycle',
    car: 'car',
    van: 'van',
    truck: 'truck',
    motorcycle: 'motorcycle',
    hgv: 'truck',
  };
  return map[frontendType] ?? frontendType.toLowerCase();
}

// Capacity Service returns 'low' | 'moderate' | 'high' | 'critical'.
// The frontend occupancyColor() function uses 'low' | 'medium' | 'high' | 'critical'.
// Map 'moderate' → 'medium' so colour indicators render correctly (U-08 fix).
function capacityLevelToFrontend(level: string): 'low' | 'medium' | 'high' | 'critical' {
  const levelMap: Record<string, 'low' | 'medium' | 'high' | 'critical'> = {
    low: 'low',
    moderate: 'medium',
    high: 'high',
    critical: 'critical',
  };
  return levelMap[level] ?? 'low';
}

// Fetches current occupancy for every registered segment from the Capacity Service.
// Called in parallel with the Journey Service fetch in getJourney() so it adds
// zero latency to the page load.  Returns an empty Map on any error so the UI
// degrades gracefully (falls back to neutral 50% / 'medium' values).
type SegmentOccupancy = { pct: number; level: 'low' | 'medium' | 'high' | 'critical' };

async function fetchSegmentOccupancyMap(): Promise<Map<string, SegmentOccupancy>> {
  if (occupancyCacheEntry && occupancyCacheEntry.expiresAt > Date.now()) {
    return occupancyCacheEntry.value;
  }

  try {
    const res = await fetch(`${BASE_URL}/api/v1/capacity/segments/occupancy`, {
      headers: authHeaders(),
    });
    if (!res.ok) return new Map();
    const data = await res.json();
    const map = new Map<string, SegmentOccupancy>();
    (Array.isArray(data) ? data : []).forEach((seg: any) => {
      map.set(seg.segment_id, {
        pct: seg.occupancy_pct ?? 0,
        level: capacityLevelToFrontend(seg.level ?? 'low'),
      });
    });
    occupancyCacheEntry = {
      value: map,
      expiresAt: Date.now() + SEGMENT_OCCUPANCY_CACHE_TTL_MS,
    };
    return map;
  } catch {
    return new Map();
  }
}

function parseGeoPoint(value: any): GeoPoint | undefined {
  const lat = Number(value?.lat);
  const lng = Number(value?.lng);
  if (!Number.isFinite(lat) || !Number.isFinite(lng)) {
    return undefined;
  }
  return { lat, lng };
}

function appendUniquePoint(points: GeoPoint[], next?: GeoPoint) {
  if (!next) return;
  const last = points[points.length - 1];
  if (last && last.lat === next.lat && last.lng === next.lng) {
    return;
  }
  points.push(next);
}

function buildJourneyPath(apiSegments: any[], origin?: GeoPoint, destination?: GeoPoint): GeoPoint[] | undefined {
  const points: GeoPoint[] = [];
  const sortedSegments = [...apiSegments].sort((a, b) => {
    const aOrder = Number(a?.sequence_order ?? a?.sequence ?? 0);
    const bOrder = Number(b?.sequence_order ?? b?.sequence ?? 0);
    return aOrder - bOrder;
  });

  sortedSegments.forEach((segment) => {
    appendUniquePoint(points, parseGeoPoint({ lat: segment?.from_lat, lng: segment?.from_lng }));
    appendUniquePoint(points, parseGeoPoint({ lat: segment?.to_lat, lng: segment?.to_lng }));
  });

  if (points.length < 2) {
    appendUniquePoint(points, origin);
    appendUniquePoint(points, destination);
  }

  return points.length >= 2 ? points : undefined;
}

function mapSegments(
  apiSegments: any[],
  occupancyMap?: Map<string, SegmentOccupancy>,
): RouteSegment[] {
  if (!Array.isArray(apiSegments)) return [];
  return apiSegments.map((s) => {
    const segId: string = s.segment_id ?? s.id;
    const occ = occupancyMap instanceof Map ? occupancyMap.get(segId) : undefined;
    return {
      id: segId,
      name: s.segment_name ?? s.name,
      // Use live occupancy where available; otherwise mark as unknown.
      occupancy: occ !== undefined ? Math.round(occ.pct) : 0,
      occupancyKnown: occ !== undefined,
      level: occ !== undefined ? occ.level : ('low' as const),
      sequenceOrder: s.sequence_order ?? s.sequence,
      traversalMinutes: s.traversal_minutes ?? s.traversal_time_minutes,
      timeWindowStart: s.time_window_start,
      timeWindowEnd: s.time_window_end,
      region: s.region,
      fromLat: typeof s.from_lat === 'number' ? s.from_lat : undefined,
      fromLng: typeof s.from_lng === 'number' ? s.from_lng : undefined,
      toLat: typeof s.to_lat === 'number' ? s.to_lat : undefined,
      toLng: typeof s.to_lng === 'number' ? s.to_lng : undefined,
    };
  });
}

function timelineTypeForEvent(eventType: string): string {
  switch ((eventType ?? '').toLowerCase()) {
    case 'journey.requested':
      return 'created';
    case 'journey.booked':
      return 'approved';
    case 'journey.rejected':
      return 'rejected';
    case 'journey.cancelled':
      return 'cancelled';
    case 'journey.activated':
      return 'active';
    case 'journey.completed':
      return 'completed';
    case 'journey.expired':
      return 'expired';
    default:
      return 'info';
  }
}

function fallbackTimelineLabel(eventType: string): string {
  switch ((eventType ?? '').toLowerCase()) {
    case 'journey.requested':
      return 'Journey booking requested';
    case 'journey.booked':
      return 'Journey approved';
    case 'journey.rejected':
      return 'Journey rejected';
    case 'journey.cancelled':
      return 'Journey cancelled';
    case 'journey.activated':
      return 'Journey activated';
    case 'journey.completed':
      return 'Journey completed';
    case 'journey.expired':
      return 'Journey expired';
    default:
      return 'Journey event';
  }
}

function timelineActor(actorType: string, actorID?: string): string {
  const normalizedType = (actorType ?? '').toLowerCase();
  const normalizedID = (actorID ?? '').trim();
  if (normalizedType === 'driver') {
    return normalizedID ? `Driver ${normalizedID}` : 'Driver';
  }
  if (normalizedType === 'admin') {
    return normalizedID ? `Admin ${normalizedID}` : 'Admin';
  }
  if (normalizedType === 'system') {
    return normalizedID || 'System';
  }
  return normalizedID || normalizedType || 'System';
}

function mapJourneyEvent(ev: APIJourneyEvent, index: number): TimelineEvent {
  const eventType = ev.event_type ?? '';
  const note = (ev.note ?? '').trim();
  return {
    id: (ev.event_id ?? '').trim() || `E${index + 1}`,
    type: timelineTypeForEvent(eventType),
    label: note || fallbackTimelineLabel(eventType),
    timestamp: ev.timestamp ?? new Date().toISOString(),
    by: timelineActor(ev.actor_type ?? 'system', ev.actor_id),
  };
}

function resolveJourneyPlaceLabel(placeID: unknown, coords?: GeoPoint, fallbackName?: unknown): string {
  if (coords) {
    return getLocationName(coords.lat, coords.lng);
  }

  const normalizedPlaceID = typeof placeID === 'string' ? placeID.trim() : '';
  if (normalizedPlaceID && !normalizedPlaceID.toLowerCase().startsWith('coord_')) {
    return normalizedPlaceID;
  }

  const normalizedFallback = typeof fallbackName === 'string' ? fallbackName.trim() : '';
  if (normalizedFallback) {
    return normalizedFallback;
  }

  return 'Unknown';
}

export async function getJourneyEvents(id: string, isAdmin = false): Promise<TimelineEvent[]> {
  const path = isAdmin
    ? `/api/v1/admin/journeys/${id}/events`
    : `/api/v1/journeys/${id}/events`;
  const events = await apiFetch<APIJourneyEvent[]>(path);
  return (Array.isArray(events) ? events : []).map(mapJourneyEvent);
}

function mapApiJourney(
  j: any,
  occupancyMap?: Map<string, SegmentOccupancy>,
  timeline?: TimelineEvent[],
): Journey {
  const originCoords = parseGeoPoint(j.origin);
  const destinationCoords = parseGeoPoint(j.destination);
  const totalDistanceKm = Number.isFinite(Number(j.total_distance_km)) ? Number(j.total_distance_km) : undefined;
  const apiDurationMinutes = Number.isFinite(Number(j.total_duration_minutes))
    ? Number(j.total_duration_minutes)
    : undefined;

  const segments = mapSegments(j.segments ?? [], occupancyMap);
  const fallbackDurationMinutes = segments.reduce((total, segment) => {
    return total + (segment.traversalMinutes ?? 0);
  }, 0);
  const totalDurationMinutes = apiDurationMinutes && apiDurationMinutes > 0
    ? apiDurationMinutes
    : (fallbackDurationMinutes > 0 ? fallbackDurationMinutes : undefined);

  const mapPath = buildJourneyPath(j.segments ?? [], originCoords, destinationCoords);
  const originName = resolveJourneyPlaceLabel(j.origin_place_id, originCoords, j.origin_name);
  const destName = resolveJourneyPlaceLabel(j.destination_place_id, destinationCoords, j.destination_name);

  const driverId = j.driver_id ?? '';
  const driverName = (j.driver_name ?? '').trim() || fallbackDriverName(driverId);

  return {
    id: j.journey_id,
    driverId,
    driverName,
    origin: originName,
    destination: destName,
    departureTime: j.departure_time ?? '',
    estimatedArrival: j.estimated_arrival ?? j.departure_time ?? '',
    vehicleType: mapVehicleType(j.vehicle_type ?? 'car'),
    status: mapStatus(j.status ?? 'pending'),
    region: 'Central',
    rejectionReason: j.rejection_reason,
    segments,
    originCoords,
    destinationCoords,
    originPlaceId: j.origin_place_id,
    destinationPlaceId: j.destination_place_id,
    totalDistanceKm,
    totalDurationMinutes,
    mapPath,
    timeline: timeline ?? [],
    createdAt: j.created_at ?? new Date().toISOString(),
    updatedAt: j.updated_at ?? new Date().toISOString(),
    distance: totalDistanceKm !== undefined ? `${totalDistanceKm.toFixed(1)} km` : '-',
    duration: totalDurationMinutes !== undefined ? `${Math.round(totalDurationMinutes)} min` : '-',
  };
}

async function apiFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...(options.headers ?? {}),
    },
  });

  const json = await res.json();
  if (!res.ok || json.success === false) {
    throw new Error(json.error?.message ?? `HTTP ${res.status}`);
  }
  return json.data as T;
}

// ---- Public API functions ----

export async function createJourney(params: {
  origin: string;
  destination: string;
  originCoords?: { lat: number; lng: number };
  destCoords?: { lat: number; lng: number };
  originPlaceId?: string;
  destinationPlaceId?: string;
  departureTime: string;
  vehicleType: string;
  priorityLevel?: 'normal' | 'max';
}): Promise<Journey> {
  const originCoords = params.originCoords ?? getCoordinates(params.origin);
  const destCoords = params.destCoords ?? getCoordinates(params.destination);

  const idempotencyKey = generateIdempotencyKey();
  const departureISO = new Date(params.departureTime).toISOString();

  const data = await apiFetch<any>('/api/v1/journeys', {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify({
      origin: originCoords,
      destination: destCoords,
      origin_place_id: params.originPlaceId,
      destination_place_id: params.destinationPlaceId,
      departure_time: departureISO,
      vehicle_type: toApiVehicleType(params.vehicleType),
      priority_level: params.priorityLevel ?? 'normal',
    }),
  });

  const initialJourney = mapApiJourney(data);
  let timeline: TimelineEvent[] = [];
  try {
    timeline = await getJourneyEvents(initialJourney.id);
  } catch {
    // Keep journey usable even if timeline endpoint is temporarily unavailable.
  }
  const journey = mapApiJourney(data, undefined, timeline);
  // Preserve the human-readable names the user entered
  journey.origin = params.origin;
  journey.destination = params.destination;
  journey.originCoords = originCoords;
  journey.destinationCoords = destCoords;
  if (params.originPlaceId) {
    journey.originPlaceId = params.originPlaceId;
  }
  if (params.destinationPlaceId) {
    journey.destinationPlaceId = params.destinationPlaceId;
  }
  invalidateJourneyReadCaches();
  writeCache(journeyDetailCache, `${cacheScope()}|journey:${journey.id}`, journey, JOURNEY_DETAIL_CACHE_TTL_MS);
  return journey;
}

export async function listJourneys(
  _driverID: string,
  statusFilter?: string,
  page = 1,
  limit = 20,
): Promise<{ journeys: Journey[]; total: number }> {
  const key = `${cacheScope()}|driver-list|status=${statusFilter ?? ''}|page=${page}|limit=${limit}`;
  const cached = readCache(journeyListCache, key);
  if (cached) {
    return cached;
  }

  const params = new URLSearchParams();
  if (statusFilter) params.set('status', statusFilter);
  params.set('page', String(page));
  params.set('limit', String(limit));

  const [data, occupancyMap] = await Promise.all([
    apiFetch<any>(`/api/v1/journeys?${params}`),
    fetchSegmentOccupancyMap(),
  ]);
  return writeCache(journeyListCache, key, {
    journeys: (data.journeys ?? []).map((j: any) => mapApiJourney(j, occupancyMap)),
    total: data.total ?? 0,
  }, JOURNEY_LIST_CACHE_TTL_MS);
}

export async function getJourney(id: string): Promise<Journey> {
  const key = `${cacheScope()}|journey:${id}`;
  const cached = readCache(journeyDetailCache, key);
  if (cached) {
    return cached;
  }

  // Fetch journey data and current segment occupancy in parallel so the
  // occupancy call adds zero latency to the page load (U-08 fix).
  const [data, occupancyMap, timeline] = await Promise.all([
    apiFetch<any>(`/api/v1/journeys/${id}`),
    fetchSegmentOccupancyMap(),
    getJourneyEvents(id),
  ]);
  return writeCache(journeyDetailCache, key, mapApiJourney(data, occupancyMap, timeline), JOURNEY_DETAIL_CACHE_TTL_MS);
}

export async function cancelJourney(id: string): Promise<Journey> {
  const [data, occupancyMap, timeline] = await Promise.all([
    apiFetch<any>(`/api/v1/journeys/${id}/cancel`, { method: 'PUT' }),
    fetchSegmentOccupancyMap(),
    getJourneyEvents(id),
  ]);
  const journey = mapApiJourney(data, occupancyMap, timeline);
  invalidateJourneyReadCaches(id);
  writeCache(journeyDetailCache, `${cacheScope()}|journey:${journey.id}`, journey, JOURNEY_DETAIL_CACHE_TTL_MS);
  return journey;
}

export async function activateJourney(id: string): Promise<Journey> {
  const [data, occupancyMap, timeline] = await Promise.all([
    apiFetch<any>(`/api/v1/journeys/${id}/activate`, { method: 'PUT' }),
    fetchSegmentOccupancyMap(),
    getJourneyEvents(id),
  ]);
  const journey = mapApiJourney(data, occupancyMap, timeline);
  invalidateJourneyReadCaches(id);
  writeCache(journeyDetailCache, `${cacheScope()}|journey:${journey.id}`, journey, JOURNEY_DETAIL_CACHE_TTL_MS);
  return journey;
}

export async function completeJourney(id: string): Promise<Journey> {
  const [data, occupancyMap, timeline] = await Promise.all([
    apiFetch<any>(`/api/v1/journeys/${id}/complete`, { method: 'PUT' }),
    fetchSegmentOccupancyMap(),
    getJourneyEvents(id),
  ]);
  const journey = mapApiJourney(data, occupancyMap, timeline);
  invalidateJourneyReadCaches(id);
  writeCache(journeyDetailCache, `${cacheScope()}|journey:${journey.id}`, journey, JOURNEY_DETAIL_CACHE_TTL_MS);
  return journey;
}

export async function adminListJourneys(filters?: {
  status?: string;
  driverId?: string;
  page?: number;
  limit?: number;
}): Promise<{ journeys: Journey[]; total: number }> {
  const key = `${cacheScope()}|admin-list|status=${filters?.status ?? ''}|driver=${filters?.driverId ?? ''}|page=${filters?.page ?? 1}|limit=${filters?.limit ?? 20}`;
  const cached = readCache(adminJourneyListCache, key);
  if (cached) {
    return cached;
  }

  const params = new URLSearchParams();
  if (filters?.status) params.set('status', filters.status);
  if (filters?.driverId) params.set('driver_id', filters.driverId);
  if (filters?.page) params.set('page', String(filters.page));
  if (filters?.limit) params.set('limit', String(filters.limit));

  const [data, occupancyMap] = await Promise.all([
    apiFetch<any>(`/api/v1/admin/journeys?${params}`),
    fetchSegmentOccupancyMap(),
  ]);

  const rawJourneys = Array.isArray(data.journeys) ? data.journeys : [];
  const missingDriverIDs = rawJourneys
    .filter((j: any) => !String(j?.driver_name ?? '').trim())
    .map((j: any) => String(j?.driver_id ?? '').trim())
    .filter((driverID: string) => driverID !== '');
  const resolvedNames = await resolveDriverNames(missingDriverIDs);

  const hydratedJourneys = rawJourneys.map((j: any) => {
    const driverID = String(j?.driver_id ?? '').trim();
    if (!driverID) return j;
    if (String(j?.driver_name ?? '').trim()) return j;
    const resolvedName = resolvedNames.get(driverID);
    if (!resolvedName) return j;
    return { ...j, driver_name: resolvedName };
  });

  return writeCache(adminJourneyListCache, key, {
    journeys: hydratedJourneys.map((j: any) => mapApiJourney(j, occupancyMap)),
    total: data.total ?? 0,
  }, JOURNEY_LIST_CACHE_TTL_MS);
}

export async function adminCancelJourney(id: string): Promise<Journey> {
  const [data, occupancyMap, timeline] = await Promise.all([
    apiFetch<any>(`/api/v1/admin/journeys/${id}/cancel`, { method: 'PUT' }),
    fetchSegmentOccupancyMap(),
    getJourneyEvents(id, true),
  ]);
  const journey = mapApiJourney(data, occupancyMap, timeline);
  invalidateJourneyReadCaches(id);
  writeCache(journeyDetailCache, `${cacheScope()}|journey:${journey.id}`, journey, JOURNEY_DETAIL_CACHE_TTL_MS);
  return journey;
}

export interface EnforcementVerifyResult {
  authorized: boolean;
  journey_id?: string;
  driver_id?: string;
  status?: string;
  segment_id: string;
  time_window_start?: string;
  time_window_end?: string;
  timestamp: string;
}

export interface AdminAnalyticsResult {
  total_journeys: number;
  approved: number;
  rejected: number;
  active: number;
  completed: number;
  cancelled: number;
  expired: number;
  approval_rate: number;
  rejection_rate: number;
  window: string;
}

// adminAnalytics fetches real journey counts from the backend (U-14).
// window: '1h' | '24h' | '7d'
export async function adminAnalytics(window = '24h'): Promise<AdminAnalyticsResult> {
  return apiFetch<AdminAnalyticsResult>(`/api/v1/admin/analytics?window=${window}`);
}

export async function enforcementVerify(params: {
  segmentId: string;
  vehiclePlate?: string;
  timestamp?: string;
}): Promise<EnforcementVerifyResult> {
  const q = new URLSearchParams({ segment_id: params.segmentId });
  if (params.vehiclePlate) q.set('vehicle_plate', params.vehiclePlate);
  if (params.timestamp) q.set('timestamp', params.timestamp);
  return apiFetch<EnforcementVerifyResult>(`/api/v1/enforcement/verify?${q}`);
}
