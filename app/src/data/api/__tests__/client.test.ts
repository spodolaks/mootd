import { ApiError, authFetch } from '../client';

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
