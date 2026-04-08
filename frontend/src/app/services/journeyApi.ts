import { Journey, JourneyStatus, RouteSegment, VehicleType } from '../types';
import { authHeaders } from './auth';
import { getCoordinates, getLocationName } from './coordinates';

// Prefer relative URLs (nginx will proxy to the right backend). For local/dev
// you can set VITE_API_URL to point at a specific gateway or host.
const BASE_URL = import.meta.env.VITE_API_URL ?? '';

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
    return map;
  } catch {
    return new Map();
  }
}

function mapSegments(
  apiSegments: any[],
  occupancyMap?: Map<string, SegmentOccupancy>,
): RouteSegment[] {
  if (!Array.isArray(apiSegments)) return [];
  return apiSegments.map((s) => {
    const segId: string = s.segment_id ?? s.id;
    const occ = occupancyMap?.get(segId);
    return {
      id: segId,
      name: s.segment_name ?? s.name,
      // U-08: use live occupancy from Capacity Service; fall back to neutral 50%
      // only when the occupancy fetch failed or this segment_id is unrecognised.
      occupancy: occ !== undefined ? Math.round(occ.pct) : 50,
      level: occ !== undefined ? occ.level : ('medium' as const),
      sequenceOrder: s.sequence_order ?? s.sequence,
      traversalMinutes: s.traversal_minutes ?? s.traversal_time_minutes,
      timeWindowStart: s.time_window_start,
      timeWindowEnd: s.time_window_end,
      region: s.region,
    };
  });
}

function mapApiJourney(j: any, occupancyMap?: Map<string, SegmentOccupancy>): Journey {
  const originName = j.origin
    ? getLocationName(j.origin.lat, j.origin.lng)
    : j.origin_name ?? 'Unknown';
  const destName = j.destination
    ? getLocationName(j.destination.lat, j.destination.lng)
    : j.destination_name ?? 'Unknown';

  return {
    id: j.journey_id,
    driverId: j.driver_id ?? '',
    driverName: j.driver_name ?? 'Driver',
    origin: originName,
    destination: destName,
    departureTime: j.departure_time ?? '',
    estimatedArrival: j.estimated_arrival ?? j.departure_time ?? '',
    vehicleType: mapVehicleType(j.vehicle_type ?? 'car'),
    status: mapStatus(j.status ?? 'pending'),
    region: 'Central',
    rejectionReason: j.rejection_reason,
    segments: mapSegments(j.segments ?? [], occupancyMap),
    timeline: [
      {
        id: 'T1',
        type: 'created',
        label: 'Journey booked',
        timestamp: j.created_at ?? new Date().toISOString(),
        by: 'You',
      },
      {
        id: 'T2',
        type: j.status?.toLowerCase() ?? 'pending',
        label: `Journey ${j.status?.toLowerCase() ?? 'pending'}`,
        timestamp: j.updated_at ?? new Date().toISOString(),
        by: 'System',
      },
    ],
    createdAt: j.created_at ?? new Date().toISOString(),
    updatedAt: j.updated_at ?? new Date().toISOString(),
    distance: '-',
    duration: '-',
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
      departure_time: departureISO,
      vehicle_type: toApiVehicleType(params.vehicleType),
      priority_level: params.priorityLevel ?? 'normal',
    }),
  });

  const journey = mapApiJourney(data);
  // Preserve the human-readable names the user entered
  journey.origin = params.origin;
  journey.destination = params.destination;
  return journey;
}

export async function listJourneys(
  _driverID: string,
  statusFilter?: string,
  page = 1,
  limit = 20,
): Promise<{ journeys: Journey[]; total: number }> {
  const params = new URLSearchParams();
  if (statusFilter) params.set('status', statusFilter);
  params.set('page', String(page));
  params.set('limit', String(limit));

  const data = await apiFetch<any>(`/api/v1/journeys?${params}`);
  return {
    journeys: (data.journeys ?? []).map(mapApiJourney),
    total: data.total ?? 0,
  };
}

export async function getJourney(id: string): Promise<Journey> {
  // Fetch journey data and current segment occupancy in parallel so the
  // occupancy call adds zero latency to the page load (U-08 fix).
  const [data, occupancyMap] = await Promise.all([
    apiFetch<any>(`/api/v1/journeys/${id}`),
    fetchSegmentOccupancyMap(),
  ]);
  return mapApiJourney(data, occupancyMap);
}

export async function cancelJourney(id: string): Promise<Journey> {
  const data = await apiFetch<any>(`/api/v1/journeys/${id}/cancel`, { method: 'PUT' });
  return mapApiJourney(data);
}

export async function activateJourney(id: string): Promise<Journey> {
  const data = await apiFetch<any>(`/api/v1/journeys/${id}/activate`, { method: 'PUT' });
  return mapApiJourney(data);
}

export async function completeJourney(id: string): Promise<Journey> {
  const data = await apiFetch<any>(`/api/v1/journeys/${id}/complete`, { method: 'PUT' });
  return mapApiJourney(data);
}

export async function adminListJourneys(filters?: {
  status?: string;
  driverId?: string;
  page?: number;
  limit?: number;
}): Promise<{ journeys: Journey[]; total: number }> {
  const params = new URLSearchParams();
  if (filters?.status) params.set('status', filters.status);
  if (filters?.driverId) params.set('driver_id', filters.driverId);
  if (filters?.page) params.set('page', String(filters.page));
  if (filters?.limit) params.set('limit', String(filters.limit));

  const data = await apiFetch<any>(`/api/v1/admin/journeys?${params}`);
  return {
    journeys: (data.journeys ?? []).map(mapApiJourney),
    total: data.total ?? 0,
  };
}

export async function adminCancelJourney(id: string): Promise<Journey> {
  const data = await apiFetch<any>(`/api/v1/admin/journeys/${id}/cancel`, { method: 'PUT' });
  return mapApiJourney(data);
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
