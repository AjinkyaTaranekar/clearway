/**
 * Token storage helpers.
 * Tokens are issued by the IAM service and stored in localStorage.
 * No JWT is ever generated client-side.
 */

const ACCESS_TOKEN_KEY = 'cw_token';
const REFRESH_TOKEN_KEY = 'cw_refresh';

/** Persist both tokens returned by the IAM service after login / register / refresh. */
export function storeTokens(accessToken: string, refreshToken: string): void {
  localStorage.setItem(ACCESS_TOKEN_KEY, accessToken);
  localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken);
}

/** Return the current RS256 access token, or null if not authenticated. */
export function getToken(): string | null {
  return localStorage.getItem(ACCESS_TOKEN_KEY);
}

/** Return the opaque refresh token, or null if not stored. */
export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_TOKEN_KEY);
}

/** Remove both tokens from storage (on logout or session expiry). */
export function clearTokens(): void {
  localStorage.removeItem(ACCESS_TOKEN_KEY);
  localStorage.removeItem(REFRESH_TOKEN_KEY);
}

// Backward-compat alias used by older callers.
export const clearToken = clearTokens;

/** Build an Authorization header from the stored access token. */
export function authHeaders(): Record<string, string> {
  const token = getToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}
