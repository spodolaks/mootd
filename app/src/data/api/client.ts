import { Platform } from 'react-native';

// On web the browser enforces CORS: localhost and 127.0.0.1 are different origins.
// Use localhost on web so requests stay same-origin with the Expo dev server.
const DEFAULT_API_BASE_URL =
  Platform.OS === 'web' ? 'http://localhost:8089' : 'http://127.0.0.1:8089';
const DEFAULT_TIMEOUT_MS = 10_000; // 10 seconds

export class ApiError extends Error {
  status: number;
  details: unknown;

  constructor(message: string, status: number, details: unknown = null) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.details = details;
  }
}

const normalizeBaseURL = (url: string): string => url.replace(/\/+$/, '');

const API_BASE_URL = normalizeBaseURL(
  process.env.EXPO_PUBLIC_API_URL || DEFAULT_API_BASE_URL
);

const buildURL = (endpoint: string): string => {
  if (endpoint.startsWith('http://') || endpoint.startsWith('https://')) {
    return endpoint;
  }
  return `${API_BASE_URL}${endpoint.startsWith('/') ? endpoint : `/${endpoint}`}`;
};

const parseResponseBody = async (response: Response): Promise<unknown> => {
  if (response.status === 204) {
    return null;
  }

  const contentType = response.headers.get('content-type') || '';
  if (contentType.includes('application/json')) {
    try {
      return await response.json();
    } catch {
      return null;
    }
  }

  try {
    return await response.text();
  } catch {
    return null;
  }
};

// In-memory auth token — set after successful sign-in, cleared on sign-out.
let _authToken: string | null = null;

/**
 * Set the JWT to be sent in the Authorization header on every request.
 * Pass null to clear (e.g. on sign-out).
 */
export const setAuthToken = (token: string | null): void => {
  _authToken = token;
};

/** Read the current in-memory auth token (used for manual fetch calls). */
export const getAuthToken = (): string | null => _authToken;

/**
 * Merge caller-supplied headers with the default Content-Type and, when present,
 * the Authorization Bearer token.
 * If a header value is explicitly `undefined`, it is omitted — this lets
 * postFormData drop Content-Type so the runtime can set the multipart boundary.
 */
const buildHeaders = (overrides: HeadersInit | undefined): Record<string, string> => {
  const merged: Record<string, string> = { 'Content-Type': 'application/json' };

  if (_authToken) {
    merged['Authorization'] = `Bearer ${_authToken}`;
  }

  if (overrides && typeof overrides === 'object' && !Array.isArray(overrides)) {
    for (const [key, value] of Object.entries(overrides)) {
      if (value === undefined) {
        delete merged[key];
      } else {
        merged[key] = value as string;
      }
    }
  }
  return merged;
};

let isRefreshing = false;
let refreshPromise: Promise<boolean> | null = null;

const attemptRefresh = async (): Promise<boolean> => {
  if (isRefreshing && refreshPromise) {
    return refreshPromise;
  }
  isRefreshing = true;
  refreshPromise = (async () => {
    try {
      // Dynamic import to avoid circular dependency
      const { useAuthStore } = await import('@/src/store/authStore');
      const store = useAuthStore.getState();
      if (!store.session?.refreshToken) return false;
      await store.refreshSession();
      return true;
    } catch {
      return false;
    } finally {
      isRefreshing = false;
      refreshPromise = null;
    }
  })();
  return refreshPromise;
};

const request = async <T>(endpoint: string, init: RequestInit = {}, timeoutMs = DEFAULT_TIMEOUT_MS): Promise<T> => {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), timeoutMs);

  let response: Response;
  try {
    response = await fetch(buildURL(endpoint), {
      ...init,
      signal: init.signal ?? controller.signal,
      headers: buildHeaders(init.headers),
    });
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') {
      throw new ApiError(`Request timed out after ${timeoutMs}ms`, 408);
    }
    throw err;
  } finally {
    clearTimeout(timeoutId);
  }

  const body = await parseResponseBody(response);

  if (!response.ok) {
    // User-friendly message for rate limiting
    if (response.status === 429) {
      throw new ApiError('You\'re making too many requests. Please wait a moment and try again.', 429, body);
    }

    // Attempt token refresh on 401 before throwing
    if (response.status === 401 && _authToken) {
      const refreshed = await attemptRefresh();
      if (refreshed) {
        // Retry the original request with the new token
        const retryResponse = await fetch(buildURL(endpoint), {
          ...init,
          headers: buildHeaders(init.headers),
        });
        const retryBody = await parseResponseBody(retryResponse);
        if (retryResponse.ok) return retryBody as T;
        // If retry also fails, fall through to error handling
      }
      // Refresh failed — clear auth and throw
      const { useAuthStore } = await import('@/src/store/authStore');
      void useAuthStore.getState().signOut();
    }

    const fallbackMessage = `Request failed with status ${response.status}`;
    const apiMessage =
      body &&
      typeof body === 'object' &&
      'error' in body &&
      typeof (body as Record<string, unknown>).error === 'string'
        ? ((body as Record<string, unknown>).error as string)
        : fallbackMessage;

    throw new ApiError(apiMessage, response.status, body);
  }

  return body as T;
};

export const apiClient = {
  get: async <T>(endpoint: string, init: RequestInit = {}) =>
    request<T>(endpoint, {
      ...init,
      method: 'GET',
    }),

  post: async <T>(endpoint: string, payload: unknown, init: RequestInit = {}) =>
    request<T>(endpoint, {
      ...init,
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  put: async <T>(endpoint: string, payload: unknown, init: RequestInit = {}) =>
    request<T>(endpoint, {
      ...init,
      method: 'PUT',
      body: JSON.stringify(payload),
    }),

  patch: async <T>(endpoint: string, payload: unknown, init: RequestInit = {}) =>
    request<T>(endpoint, {
      ...init,
      method: 'PATCH',
      body: JSON.stringify(payload),
    }),

  delete: async <T>(endpoint: string, init: RequestInit = {}) =>
    request<T>(endpoint, {
      ...init,
      method: 'DELETE',
    }),

  /**
   * POST with multipart/form-data (for file uploads).
   * The Content-Type header is intentionally omitted so the runtime sets the
   * correct multipart boundary automatically.
   */
  postFormData: async <T>(endpoint: string, formData: FormData, init: RequestInit = {}) =>
    request<T>(endpoint, {
      ...init,
      method: 'POST',
      body: formData as unknown as BodyInit,
      headers: {
        // Passing undefined removes the default application/json so the
        // runtime can auto-set the multipart boundary.
        'Content-Type': undefined as unknown as string,
        ...(init.headers || {}),
      },
    }),
};

export const getApiBaseURL = (): string => API_BASE_URL;
