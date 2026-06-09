import { Platform } from 'react-native';

// Production API URL - hardcoded for web deployment
const DEFAULT_API_BASE_URL =
  Platform.OS === 'web' ? 'https://api.spodolaks.id.lv' : 'http://127.0.0.1:8089';
const DEFAULT_TIMEOUT_MS = 10_000; // 10 seconds

/**
 * Stable, machine-readable error codes returned by the backend
 * (mootd#41). Switch on these — never on the human-readable
 * `message` — for retry / UX-branch decisions.
 *
 * Adding a new code is non-breaking; renaming or removing a
 * code is a contract change.
 */
export type ApiErrorCode =
  | 'INVALID_TOKEN'
  | 'MISSING_FIELD'
  | 'INVALID_INPUT'
  | 'QUOTA_EXCEEDED'
  | 'RATE_LIMITED'
  | 'UPSTREAM_TIMEOUT'
  | 'UPSTREAM_ERROR'
  | 'NOT_FOUND'
  | 'FORBIDDEN'
  | 'CONFLICT'
  | 'SERVICE_UNAVAILABLE'
  | 'INTERNAL'
  | string; // backend may add new codes; allow forward-compat

export class ApiError extends Error {
  status: number;
  details: unknown;
  /**
   * Server-issued request ID, captured from the X-Request-ID
   * response header (mootd#38). Surfaces in toasts + Sentry
   * events for cheap log correlation when a customer reports
   * "the app crashed at 14:23 UTC". Empty when the response
   * predates the middleware or the request never reached the
   * server (network failure, timeout).
   */
  requestId: string;
  /**
   * Backend's stable error code (mootd#41). Empty when the
   * server didn't supply one (older endpoints, network errors,
   * etc.). Use `code` for retry decisions; fall back to
   * `status` for HTTP-level branching.
   */
  code: ApiErrorCode;

  constructor(
    message: string,
    status: number,
    details: unknown = null,
    requestId = '',
    code: ApiErrorCode = ''
  ) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.details = details;
    this.requestId = requestId;
    this.code = code;
  }
}

const normalizeBaseURL = (url: string): string => url.replace(/\/+$/, '');

const API_BASE_URL = normalizeBaseURL(process.env.EXPO_PUBLIC_API_URL || DEFAULT_API_BASE_URL);

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

/**
 * Auth endpoints mint or rotate tokens, so a 401 from one must NEVER drive the
 * token-refresh interceptor. Refreshing to retry the refresh call itself awaits
 * the in-flight refresh promise that is awaiting this very request — a deadlock
 * that wedges the whole API layer; and a logout 401 would loop signOut →
 * logout → signOut. Auth calls therefore bypass the 401 interceptor (#98).
 */
const isAuthEndpoint = (endpoint: string): boolean =>
  endpoint.replace(/^https?:\/\/[^/]+/, '').startsWith('/v1/auth/');

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

/**
 * One attempt against the wire. Used by `request` so we can
 * compose the 401-refresh and the safe-GET retry (mootd#45)
 * around the same primitive.
 */
const fetchOnce = async (
  endpoint: string,
  init: RequestInit,
  timeoutMs: number
): Promise<{ response: Response; body: unknown }> => {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch(buildURL(endpoint), {
      ...init,
      signal: init.signal ?? controller.signal,
      headers: buildHeaders(init.headers),
    });
    const body = await parseResponseBody(response);
    return { response, body };
  } finally {
    clearTimeout(timeoutId);
  }
};

const request = async <T>(
  endpoint: string,
  init: RequestInit = {},
  timeoutMs = DEFAULT_TIMEOUT_MS
): Promise<T> => {
  let response: Response;
  let body: unknown;
  try {
    ({ response, body } = await fetchOnce(endpoint, init, timeoutMs));
  } catch (err) {
    // mootd#45 — retry safe GETs once on a network/timeout
    // error. Idempotent by definition; the cost of one extra
    // hop is well worth surviving a flaky cell handoff.
    if (
      (init.method ?? 'GET').toUpperCase() === 'GET' &&
      !init.signal && // caller-cancelled: don't second-guess
      err instanceof Error
    ) {
      try {
        ({ response, body } = await fetchOnce(endpoint, init, timeoutMs));
      } catch (err2) {
        if (err2 instanceof Error && err2.name === 'AbortError') {
          throw new ApiError(`Request timed out after ${timeoutMs}ms`, 408);
        }
        throw err2;
      }
    } else {
      if (err instanceof Error && err.name === 'AbortError') {
        throw new ApiError(`Request timed out after ${timeoutMs}ms`, 408);
      }
      throw err;
    }
  }

  if (!response.ok) {
    // mootd#45 — retry safe GETs once on a 5xx. Backend
    // restarts, brief pool exhaustion, transient upstream
    // failures all look like 5xx to us; retrying once
    // typically clears them.
    if (
      response.status >= 500 &&
      response.status < 600 &&
      (init.method ?? 'GET').toUpperCase() === 'GET' &&
      !init.signal
    ) {
      try {
        ({ response, body } = await fetchOnce(endpoint, init, timeoutMs));
      } catch {
        // Fall through to error handling below.
      }
    }
  }

  const requestId = response.headers.get('X-Request-ID') ?? '';

  if (!response.ok) {
    // mootd#41 — pull the stable error code off the body so
    // callers can switch on it without parsing UI copy.
    const code =
      body &&
      typeof body === 'object' &&
      'code' in body &&
      typeof (body as Record<string, unknown>).code === 'string'
        ? ((body as Record<string, unknown>).code as ApiErrorCode)
        : '';

    // User-friendly message for rate limiting
    if (response.status === 429) {
      throw new ApiError(
        "You're making too many requests. Please wait a moment and try again.",
        429,
        body,
        requestId,
        code || 'RATE_LIMITED'
      );
    }

    // Attempt token refresh on 401 before throwing — but never for auth
    // endpoints, which would recurse into the refresh/logout flow (#98).
    if (response.status === 401 && _authToken && !isAuthEndpoint(endpoint)) {
      const refreshed = await attemptRefresh();
      if (refreshed) {
        // Retry the original request with the new token
        const retry = await fetchOnce(endpoint, init, timeoutMs);
        if (retry.response.ok) return retry.body as T;
        // If retry also fails, fall through to error handling
        response = retry.response;
        body = retry.body;
      } else {
        // Refresh failed — clear auth and throw
        const { useAuthStore } = await import('@/src/store/authStore');
        void useAuthStore.getState().signOut();
      }
    }

    const fallbackMessage = `Request failed with status ${response.status}`;
    const apiMessage =
      body &&
      typeof body === 'object' &&
      'error' in body &&
      typeof (body as Record<string, unknown>).error === 'string'
        ? ((body as Record<string, unknown>).error as string)
        : fallbackMessage;

    throw new ApiError(apiMessage, response.status, body, requestId, code);
  }

  return body as T;
};

/**
 * Auth-aware fetch that returns the raw `Response` with its body
 * unconsumed, so callers that need streaming (SSE outfit generation) or
 * custom parsing (the `/v1/outfits` endpoint) get the same 401
 * silent-refresh and rate-limit handling as `apiClient` instead of
 * hand-rolling a bare `fetch` that bypasses both.
 *
 * On a non-2xx it throws `ApiError` (with the friendly 429 message and
 * the stable error `code`); on success it returns the live `Response`.
 * `timeoutMs` bounds the time-to-headers only — once the response
 * headers arrive the timer is cleared, so a long-lived SSE body stream
 * is not cut off.
 */
export const authFetch = async (
  endpoint: string,
  init: RequestInit = {},
  opts: { timeoutMs?: number } = {}
): Promise<Response> => {
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;

  const run = async (): Promise<Response> => {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeoutMs);
    try {
      return await fetch(buildURL(endpoint), {
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
  };

  let response = await run();

  // 401 → silent refresh + one retry, sharing apiClient's dedup'd refresh.
  // Auth endpoints bypass this — refreshing to retry them recurses (#98).
  if (response.status === 401 && _authToken && !isAuthEndpoint(endpoint)) {
    const refreshed = await attemptRefresh();
    if (refreshed) {
      response = await run();
    } else {
      const { useAuthStore } = await import('@/src/store/authStore');
      void useAuthStore.getState().signOut();
    }
  }

  if (!response.ok) {
    const requestId = response.headers.get('X-Request-ID') ?? '';
    const body = await parseResponseBody(response);
    const code =
      body &&
      typeof body === 'object' &&
      'code' in body &&
      typeof (body as Record<string, unknown>).code === 'string'
        ? ((body as Record<string, unknown>).code as ApiErrorCode)
        : '';

    if (response.status === 429) {
      throw new ApiError(
        "You're making too many requests. Please wait a moment and try again.",
        429,
        body,
        requestId,
        code || 'RATE_LIMITED'
      );
    }

    const fallbackMessage = `Request failed with status ${response.status}`;
    const apiMessage =
      body &&
      typeof body === 'object' &&
      'error' in body &&
      typeof (body as Record<string, unknown>).error === 'string'
        ? ((body as Record<string, unknown>).error as string)
        : fallbackMessage;

    throw new ApiError(apiMessage, response.status, body, requestId, code);
  }

  return response;
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
