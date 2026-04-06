import { Journey, JourneyStatus, RouteSegment, VehicleType } from '../data/mockData';
import { authHeaders } from './auth';
import { getCoordinates, getLocationName } from './coordinates';

// Empty string = relative URLs → Vercel rewrites proxy to GCP backend (production)
// Set VITE_API_URL=http://localhost to point at local nginx, or a specific service port
const BASE_URL = import.meta.env.VITE_API_URL ?? '';

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
  };
  return map[frontendType] ?? frontendType.toLowerCase();
}

function mapSegments(apiSegments: any[]): RouteSegment[] {
  if (!Array.isArray(apiSegments)) return [];
  return apiSegments.map((s) => ({
    id: s.segment_id ?? s.id,
    name: s.segment_name ?? s.name,
    occupancy: 50,
    level: 'medium' as const,
  }));
}

function mapApiJourney(j: any): Journey {
  const originName = j.origin
    ? getLocationName(j.origin.lat, j.origin.lng)
    : j.origin_name ?? 'Unknown';
  const destName = j.destination
    ? getLocationName(j.destination.lat, j.destination.lng)
    : j.destination_name ?? 'Unknown';

  return {
    id: j.journey_id,
    driverId: j.driver_id ?? '',
    driverName: 'Driver',
    origin: originName,
    destination: destName,
    departureTime: j.departure_time ?? '',
    estimatedArrival: j.estimated_arrival ?? j.departure_time ?? '',
    vehicleType: mapVehicleType(j.vehicle_type ?? 'car'),
    status: mapStatus(j.status ?? 'pending'),
    region: 'Central',
    rejectionReason: j.rejection_reason,
    segments: mapSegments(j.segments ?? []),
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
    distance: '—',
    duration: '—',
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
  departureTime: string;
  vehicleType: string;
}): Promise<Journey> {
  const originCoords = getCoordinates(params.origin);
  const destCoords = getCoordinates(params.destination);

  const idempotencyKey = crypto.randomUUID();
  const departureISO = new Date(params.departureTime).toISOString();

  const data = await apiFetch<any>('/api/v1/journeys', {
    method: 'POST',
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify({
      origin: originCoords,
      destination: destCoords,
      departure_time: departureISO,
      vehicle_type: toApiVehicleType(params.vehicleType),
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
  const data = await apiFetch<any>(`/api/v1/journeys/${id}`);
  return mapApiJourney(data);
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
