import { authHeaders } from './auth';
import { CapacitySegment, SegmentClosure } from '../types';

const BASE_URL = import.meta.env.VITE_API_URL ?? '';

interface ApiError {
  message?: string;
}

interface ApiEnvelope<T> {
  success?: boolean;
  data?: T;
  error?: ApiError;
}

export interface CreateSegmentClosureInput {
  segment_id: string;
  duration_minutes: number;
  reason: string;
}

async function capacityFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...(options.headers ?? {}),
    },
  });

  const json = await res.json() as ApiEnvelope<T>;
  if (!res.ok || json.success === false) {
    throw new Error(json.error?.message ?? (json as any).error ?? `HTTP ${res.status}`);
  }

  return (json.data ?? json) as T;
}

export async function listCapacitySegments(): Promise<CapacitySegment[]> {
  return capacityFetch<CapacitySegment[]>('/api/v1/capacity/segments');
}

export async function listActiveClosures(): Promise<SegmentClosure[]> {
  return capacityFetch<SegmentClosure[]>('/api/v1/capacity/closures');
}

export async function createSegmentClosure(input: CreateSegmentClosureInput): Promise<SegmentClosure> {
  return capacityFetch<SegmentClosure>('/api/v1/capacity/closures', {
    method: 'POST',
    body: JSON.stringify(input),
  });
}
