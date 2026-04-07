const BASE_URL = import.meta.env.VITE_API_URL ?? '';

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

async function mapFetch<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`);
  const json = await res.json();
  if (!res.ok) throw new Error(json.error?.message ?? json.message ?? `HTTP ${res.status}`);
  return json as T;
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
