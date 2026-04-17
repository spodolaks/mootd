import { ApiError } from '../client';

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
