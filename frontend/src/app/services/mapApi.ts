import { authHeaders } from './auth';

const BASE_URL = import.meta.env.VITE_API_URL ?? '';

interface ApiError {
  message?: string;
}

interface ApiEnvelope<T> {
  success?: boolean;
  data?: T;
  error?: ApiError;
}

export interface PlaceResult {
  place_id: string;
  name: string;
  address: string;
  lat: number;
  lng: number;
}

export interface MapNode {
  node_id: string;
  label: string;
  lat: number;
  lng: number;
}

export interface MapRoute {
  origin: MapNode;
  destination: MapNode;
  total_traversal_time_minutes: number;
  segments: {
    sequence: number;
    segment_id: string;
    segment_name: string;
    from_node_id: string;
    to_node_id: string;
    traversal_time_minutes: number;
  }[];
}

export interface TrafficGraphNode {
  node_id: string;
  label: string;
  x: number;
  y: number;
}

export interface TrafficSegmentNodeRef {
  node_id: string;
  label: string;
  lat: number;
  lng: number;
}

export interface TrafficSegment {
  segment_id: string;
  name: string;
  region: string;
  level: 'low' | 'medium' | 'high' | 'critical';
  occupancy_pct: number;
  vehicles: number;
  capacity: number;
  trend: 'improving' | 'stable' | 'worsening';
  from_node: TrafficSegmentNodeRef;
  to_node: TrafficSegmentNodeRef;
}

export interface TrafficData {
  segments: TrafficSegment[];
  nodes: TrafficGraphNode[];
}

async function mapFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    headers: {
      ...authHeaders(),
      ...(options.headers ?? {}),
    },
  });
  const json = await res.json() as ApiEnvelope<T>;
  if (!res.ok || json.success === false) {
    throw new Error(json.error?.message ?? `HTTP ${res.status}`);
  }
  return (json.data ?? json) as T;
}

export async function getMapNodes(): Promise<MapNode[]> {
  const data = await mapFetch<{ nodes: MapNode[] }>('/api/v1/map/nodes');
  return data.nodes ?? [];
}

export async function getRoute(originNodeId: string, destNodeId: string): Promise<MapRoute> {
  const q = new URLSearchParams({
    origin_node_id: originNodeId,
    destination_node_id: destNodeId,
  });
  return mapFetch<MapRoute>(`/api/v1/map/route?${q}`);
}

export async function getTrafficData(): Promise<TrafficData> {
  return mapFetch<TrafficData>('/api/v1/map/traffic');
}

// getRegion returns the deployment region from the load balancer (nginx injects REGION env var).
export async function getRegion(): Promise<string> {
  const candidates = new Set<string>();
  const regionPaths = ['/api/v1/region', '/region.json'];

  regionPaths.forEach((path) => {
    if (BASE_URL) {
      candidates.add(`${BASE_URL}${path}`);
    }
    candidates.add(path);
  });

  for (const url of candidates) {
    try {
      const res = await fetch(url, { cache: 'no-store' });
      if (!res.ok) continue;

      const json = await res.json() as { region?: string };
      const region = json.region?.trim();
      if (region) {
        return region;
      }
    } catch {
      // Try the next candidate URL.
    }
  }

  return 'local';
}

// searchPlaces calls our backend geocoding proxy (Nominatim-backed).
export async function searchPlaces(query: string, limit = 5): Promise<PlaceResult[]> {
  if (!query.trim()) return [];
  const q = new URLSearchParams({ q: query, limit: String(limit) });
  // Search endpoint is public (no auth required for booking flow)
  const res = await fetch(`${BASE_URL}/api/v1/map/search?${q}`);
  const json = (await res.json()) as ApiEnvelope<{ places: PlaceResult[] }>;
  if (!res.ok || json.success === false) return [];
  return json.data?.places ?? [];
}
