/**
 * HTTP client for the IAM service.
 * All calls go through nginx (relative URL), which proxies /api/v1/auth/ → iam-service:8082.
 * The IAM service issues RS256 JWTs; tokens are stored via auth.ts helpers.
 */

// ---------------------------------------------------------------------------
// Shared response types
// ---------------------------------------------------------------------------

export interface AuthUser {
  id: string;
  name: string;
  email: string;
  role: 'driver' | 'admin';
  vehicle_type: string;
}

export interface AuthTokens {
  access_token: string;
  refresh_token: string;
  user: AuthUser;
}

export interface UserVehicle {
  id: string;
  vehicle_type: string;
  license_info: {
    license_number: string;
    license_state?: string;
    expiry_date?: string;
  };
  is_emergency_vehicle: boolean;
  is_primary: boolean;
  created_at: string;
  updated_at: string;
}

export interface RegisterParams {
  name: string;
  email: string;
  password: string;
  vehicle_type: string;
  license_info: {
    license_number: string;
    license_state?: string;
    expiry_date?: string;
  };
}

// ---------------------------------------------------------------------------
// Internal fetch wrapper
// ---------------------------------------------------------------------------

/**
 * Generic fetch wrapper for IAM endpoints.
 * IAM success responses: { success: true, data: T }
 * IAM error responses:   { error: { code, message, details? } }
 *                     or { success: false, error: { message } }
 */
async function iamFetch<T>(
  path: string,
  options: RequestInit & { authToken?: string } = {},
): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  if (options.authToken) {
    headers['Authorization'] = `Bearer ${options.authToken}`;
  }

  const res = await fetch(path, {
    ...options,
    headers: { ...headers, ...(options.headers as Record<string, string> | undefined) },
  });

  const json = await res.json().catch(() => ({}));

  if (!res.ok) {
    // IAM validation errors: { error: { message, details } }
    // Other IAM errors:      { success: false, error: { message } }
    const msg: string =
      (json as any)?.error?.message ??
      (json as any)?.message ??
      `IAM request failed (HTTP ${res.status})`;
    throw new Error(msg);
  }

  // Successful IAM responses are wrapped: { success: true, data: T }
  // Fall back to the raw body in case the wrapper is absent.
  return ((json as any).data ?? json) as T;
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Authenticate an existing user.
 * Returns RS256 access + opaque refresh tokens plus the user profile.
 */
export async function iamLogin(email: string, password: string): Promise<AuthTokens> {
  return iamFetch<AuthTokens>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  });
}

/**
 * Register a new driver account.
 * New accounts always receive the "driver" role.
 * Returns the same token/user shape as login.
 */
export async function iamRegister(params: RegisterParams): Promise<AuthTokens> {
  return iamFetch<AuthTokens>('/api/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify(params),
  });
}

/**
 * Revoke the active refresh token on the IAM service (server-side logout).
 * Requires a valid access token.  Failure is intentionally swallowed by the
 * caller — local token state is always cleared regardless.
 */
export async function iamLogout(accessToken: string, refreshToken: string): Promise<void> {
  await iamFetch<void>('/api/v1/auth/logout', {
    method: 'POST',
    body: JSON.stringify({ refresh_token: refreshToken }),
    authToken: accessToken,
  });
}

export interface UpdateProfileParams {
  name?: string;
  vehicle_type?: string;
  license_info?: {
    license_number?: string;
    expiry_date?: string;
  };
}

/**
 * Update the authenticated user's profile on the IAM service.
 * Only name, vehicle_type, and license_info can be changed; email changes
 * are not supported by the IAM service (require a separate verification flow).
 * Requires a valid access token.
 */
export async function iamUpdateProfile(
  accessToken: string,
  params: UpdateProfileParams,
): Promise<AuthUser> {
  return iamFetch<AuthUser>('/api/v1/auth/profile', {
    method: 'PUT',
    body: JSON.stringify(params),
    authToken: accessToken,
  });
}

export interface AddSecondaryVehicleParams {
  vehicle_type: string;
  license_info: {
    license_number: string;
    license_state?: string;
    expiry_date?: string;
  };
  is_emergency_vehicle?: boolean;
}

export interface UpdateVehicleParams {
  vehicle_type?: string;
  license_info?: {
    license_number: string;
    license_state?: string;
    expiry_date?: string;
  };
  is_emergency_vehicle?: boolean;
}

export async function iamListVehicles(accessToken: string): Promise<UserVehicle[]> {
  return iamFetch<UserVehicle[]>('/api/v1/auth/vehicles', {
    method: 'GET',
    authToken: accessToken,
  });
}

export async function iamUpdatePrimaryVehicle(
  accessToken: string,
  params: UpdateVehicleParams,
): Promise<UserVehicle> {
  return iamFetch<UserVehicle>('/api/v1/auth/vehicles/primary', {
    method: 'PUT',
    body: JSON.stringify(params),
    authToken: accessToken,
  });
}

export async function iamAddSecondaryVehicle(
  accessToken: string,
  params: AddSecondaryVehicleParams,
): Promise<UserVehicle> {
  return iamFetch<UserVehicle>('/api/v1/auth/vehicles/secondary', {
    method: 'POST',
    body: JSON.stringify(params),
    authToken: accessToken,
  });
}

export async function iamUpdateSecondaryVehicle(
  accessToken: string,
  vehicleID: string,
  params: UpdateVehicleParams,
): Promise<UserVehicle> {
  return iamFetch<UserVehicle>(`/api/v1/auth/vehicles/secondary/${vehicleID}`, {
    method: 'PUT',
    body: JSON.stringify(params),
    authToken: accessToken,
  });
}

export async function iamDeleteSecondaryVehicle(accessToken: string, vehicleID: string): Promise<void> {
  await iamFetch<void>(`/api/v1/auth/vehicles/secondary/${vehicleID}`, {
    method: 'DELETE',
    authToken: accessToken,
  });
}

/**
 * Exchange an expiring access token for a fresh pair using the refresh token.
 */
export async function iamRefresh(
  refreshToken: string,
): Promise<{ access_token: string; refresh_token: string }> {
  return iamFetch<{ access_token: string; refresh_token: string }>('/api/v1/auth/refresh', {
    method: 'POST',
    body: JSON.stringify({ refresh_token: refreshToken }),
  });
}
