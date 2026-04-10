import { authHeaders } from './auth';

const BASE_URL = import.meta.env.VITE_API_URL ?? '';

interface ApiEnvelope<T> {
  success?: boolean;
  data?: T;
  error?: {
    message?: string;
  };
}

export interface ApiNotification {
  id: string;
  journey_id: string;
  title: string;
  message: string;
  type: 'info' | 'success' | 'warning' | 'error';
  read: boolean;
  read_at: string | null;
  timestamp: string;
}

export interface NotificationListResponse {
  notifications: ApiNotification[];
  unread_count: number;
  total: number;
  page: number;
  limit: number;
}

export interface AdminApiNotification extends ApiNotification {
  driver_id: string;
  event_id: string;
  event_type: string;
  delivery_status: string;
  retry_count?: number;
  last_error?: string;
}

export interface AdminNotificationListResponse {
  notifications: AdminApiNotification[];
  total: number;
  page: number;
  limit: number;
}

async function notifFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
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
    throw new Error(json.error?.message ?? `HTTP ${res.status}`);
  }
  return (json.data ?? json) as T;
}

export async function listNotifications(page = 1, limit = 50): Promise<NotificationListResponse> {
  const q = new URLSearchParams({ page: String(page), limit: String(limit) });
  return notifFetch<NotificationListResponse>(`/api/v1/notifications?${q}`);
}

export async function listAdminNotifications(page = 1, limit = 50): Promise<AdminNotificationListResponse> {
  const q = new URLSearchParams({ page: String(page), limit: String(limit) });
  return notifFetch<AdminNotificationListResponse>(`/api/v1/admin/notifications?${q}`);
}

export async function markNotificationRead(id: string): Promise<void> {
  await notifFetch(`/api/v1/notifications/${id}/read`, { method: 'PUT' });
}

export async function markAllNotificationsRead(): Promise<void> {
  await notifFetch('/api/v1/notifications/read-all', { method: 'PUT' });
}

export async function registerDeviceToken(
  fcmToken: string,
  platform: 'web' | 'android' | 'ios',
): Promise<void> {
  await notifFetch('/api/v1/notifications/device-token', {
    method: 'POST',
    body: JSON.stringify({ fcm_token: fcmToken, platform }),
  });
}

export async function deactivateDeviceToken(
  fcmToken: string,
  reason = 'driver_disabled',
): Promise<void> {
  await notifFetch('/api/v1/notifications/device-token', {
    method: 'DELETE',
    body: JSON.stringify({ fcm_token: fcmToken, reason }),
  });
}
