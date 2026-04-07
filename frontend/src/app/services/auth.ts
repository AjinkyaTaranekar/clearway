const TOKEN_KEY = 'cw_token';
const REFRESH_KEY = 'cw_refresh';

const BASE_URL = import.meta.env.VITE_API_URL ?? '';

export interface AuthUser {
  id: string;
  name: string;
  email: string;
  role: 'driver' | 'admin';
  vehicle_type?: string;
}

export interface AuthResult {
  access_token: string;
  refresh_token: string;
  user: AuthUser;
}

async function iamPost<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  const json = await res.json();
  if (!res.ok) {
    throw new Error(json.error?.message ?? json.message ?? `HTTP ${res.status}`);
  }
  // IAM service wraps response in { success, data }
  return (json.data ?? json) as T;
}

export async function loginApi(email: string, password: string): Promise<AuthResult> {
  const result = await iamPost<AuthResult>('/api/v1/auth/login', { email, password });
  storeTokens(result.access_token, result.refresh_token);
  return result;
}

export async function registerApi(params: {
  name: string;
  email: string;
  password: string;
  vehicle_type: string;
  license_number: string;
}): Promise<AuthResult> {
  const result = await iamPost<AuthResult>('/api/v1/auth/register', {
    name: params.name,
    email: params.email,
    password: params.password,
    vehicle_type: params.vehicle_type,
    license_info: { license_number: params.license_number },
  });
  storeTokens(result.access_token, result.refresh_token);
  return result;
}

export async function refreshTokens(): Promise<string> {
  const refresh = getRefreshToken();
  if (!refresh) throw new Error('No refresh token');
  const result = await iamPost<{ access_token: string; refresh_token: string }>(
    '/api/v1/auth/refresh',
    { refresh_token: refresh },
  );
  storeTokens(result.access_token, result.refresh_token);
  return result.access_token;
}

export async function logoutApi(): Promise<void> {
  const refresh = getRefreshToken();
  const token = getToken();
  if (!refresh) return;
  try {
    await fetch(`${BASE_URL}/api/v1/auth/logout`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({ refresh_token: refresh }),
    });
  } catch {
    // ignore logout errors — clear local state regardless
  }
}

export function storeTokens(access: string, refresh: string): void {
  localStorage.setItem(TOKEN_KEY, access);
  localStorage.setItem(REFRESH_KEY, refresh);
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_KEY);
}

export function clearTokens(): void {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(REFRESH_KEY);
}

// Keep backward-compat alias used by journeyApi
export const clearToken = clearTokens;

export function authHeaders(): Record<string, string> {
  const token = getToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}
