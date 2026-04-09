import { authHeaders } from './auth';

const BASE_URL = import.meta.env.VITE_API_URL ?? '';

export interface SegmentClosure {
  closure_id: string;
  segment_id: string;
  segment_name: string;
  reason: string;
  starts_at: string;
  ends_at?: string;
  is_active: boolean;
  created_at: string;
  created_by: string;
}

export interface CreateClosureParams {
  segment_id: string;
  reason: string;
  starts_at: string;
  ends_at?: string;
}

export async function listClosures(): Promise<SegmentClosure[]> {
  const res = await fetch(`${BASE_URL}/api/v1/capacity/closures`, {
    headers: authHeaders(),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error ?? `HTTP ${res.status}`);
  }
  return res.json();
}

export async function createClosure(params: CreateClosureParams): Promise<SegmentClosure> {
  const res = await fetch(`${BASE_URL}/api/v1/capacity/closures`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
    },
    body: JSON.stringify(params),
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(body.error ?? `HTTP ${res.status}`);
  }
  return body as SegmentClosure;
}
