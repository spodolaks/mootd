import { useAuthStore } from '../authStore';

// Reset store between tests
beforeEach(() => {
  useAuthStore.setState({
    user: null,
    session: null,
    isLoading: false,
    isAuthenticated: false,
    sessionRestored: false,
    error: null,
  });
});

describe('authStore', () => {
  it('starts with unauthenticated state', () => {
    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.user).toBeNull();
    expect(state.session).toBeNull();
    expect(state.sessionRestored).toBe(false);
    expect(state.error).toBeNull();
  });

  it('clearError resets error to null', () => {
    useAuthStore.setState({ error: 'something went wrong' });
    expect(useAuthStore.getState().error).toBe('something went wrong');

    useAuthStore.getState().clearError();
    expect(useAuthStore.getState().error).toBeNull();
  });

  it('signOut clears user and session', async () => {
    useAuthStore.setState({
      user: { id: '1', email: 'test@test.com', name: 'Test' },
      session: {
        accessToken: 'tok',
        expiresAt: '2099-01-01T00:00:00Z',
        user: { id: '1', email: 'test@test.com', name: 'Test' },
        mode: 'mock',
      },
      isAuthenticated: true,
    });

    await useAuthStore.getState().signOut();

    const state = useAuthStore.getState();
    expect(state.isAuthenticated).toBe(false);
    expect(state.user).toBeNull();
    expect(state.session).toBeNull();
  });
});
