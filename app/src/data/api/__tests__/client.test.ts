import { ApiError, apiClient, authFetch, setAuthToken } from '../client';

describe('ApiError', () => {
  it('stores status and details', () => {
    const err = new ApiError('not found', 404, { reason: 'missing' });
    expect(err.message).toBe('not found');
    expect(err.status).toBe(404);
    expect(err.details).toEqual({ reason: 'missing' });
    expect(err.name).toBe('ApiError');
    expect(err).toBeInstanceOf(Error);
  });

  it('defaults details to null', () => {
    const err = new ApiError('fail', 500);
    expect(err.details).toBeNull();
  });
});

describe('authFetch', () => {
  const originalFetch = global.fetch;

  const jsonResponse = (status: number, body: Record<string, unknown>): Partial<Response> => ({
    ok: status >= 200 && status < 300,
    status,
    headers: {
      get: (k: string) => (k.toLowerCase() === 'content-type' ? 'application/json' : null),
    } as unknown as Headers,
    json: async () => body,
    text: async () => JSON.stringify(body),
  });

  const mockFetch = (factory: () => Partial<Response>) => {
    (global as unknown as { fetch: unknown }).fetch = jest.fn(async () => factory());
  };

  afterEach(() => {
    (global as unknown as { fetch: typeof fetch }).fetch = originalFetch;
  });

  it('returns the raw Response on 2xx without consuming the body', async () => {
    const res = jsonResponse(200, { outfits: [] });
    const jsonSpy = jest.spyOn(res, 'json');
    mockFetch(() => res);

    const out = await authFetch('/v1/outfits', { method: 'GET' });

    expect(out).toBe(res);
    // Body must be left untouched so streaming/custom-parse callers can read it.
    expect(jsonSpy).not.toHaveBeenCalled();
  });

  it('maps 429 to a friendly ApiError with a stable code', async () => {
    mockFetch(() => jsonResponse(429, { error: 'rate limit exceeded', code: 'RATE_LIMITED' }));

    await expect(authFetch('/v1/outfits/generate', { method: 'POST' })).rejects.toMatchObject({
      status: 429,
      code: 'RATE_LIMITED',
    });
  });

  it('surfaces the friendly rate-limit message on 429', async () => {
    mockFetch(() => jsonResponse(429, { error: 'rate limit exceeded' }));

    await expect(authFetch('/v1/outfits/generate', { method: 'POST' })).rejects.toThrow(
      /too many requests/i
    );
  });

  it('throws ApiError with the server message on other non-2xx', async () => {
    mockFetch(() => jsonResponse(500, { error: 'boom' }));

    const err = await authFetch('/v1/outfits', { method: 'GET' }).catch(e => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(500);
    expect(err.message).toBe('boom');
  });
});

describe('401 interceptor — auth endpoints bypass refresh (#98)', () => {
  const originalFetch = global.fetch;

  const resp = (status: number, body: Record<string, unknown>): Partial<Response> => ({
    ok: status >= 200 && status < 300,
    status,
    headers: { get: () => null } as unknown as Headers,
    json: async () => body,
    text: async () => JSON.stringify(body),
  });

  afterEach(() => {
    (global as unknown as { fetch: typeof fetch }).fetch = originalFetch;
    setAuthToken(null);
  });

  it('does not refresh-retry a 401 from /v1/auth/refresh (no recursion/deadlock)', async () => {
    setAuthToken('stale-access-token'); // would otherwise satisfy the _authToken guard
    const fetchMock = jest.fn(async () => resp(401, { error: 'invalid refresh token' }));
    (global as unknown as { fetch: unknown }).fetch = fetchMock;

    await expect(
      apiClient.post('/v1/auth/refresh', { refreshToken: 'expired' })
    ).rejects.toMatchObject({ status: 401 });

    // Exactly one network call — the interceptor did not re-enter the refresh flow.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('does not refresh-retry a 401 from /v1/auth/logout', async () => {
    setAuthToken('stale-access-token');
    const fetchMock = jest.fn(async () => resp(401, { error: 'nope' }));
    (global as unknown as { fetch: unknown }).fetch = fetchMock;

    await expect(authFetch('/v1/auth/logout', { method: 'POST' })).rejects.toMatchObject({
      status: 401,
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
