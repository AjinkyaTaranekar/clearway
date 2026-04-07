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
