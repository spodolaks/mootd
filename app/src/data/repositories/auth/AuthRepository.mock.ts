import type {
  AuthSession,
  GoogleOAuthParams,
  IAuthRepository,
} from '@/src/domain';

export class MockAuthRepository implements IAuthRepository {
  private delay(ms = 800): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }

  async signInWithGoogleMock(): Promise<AuthSession> {
    await this.delay();
    return {
      accessToken: 'mock_access_token_local',
      expiresAt: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
      user: {
        id: 'user_mock_001',
        email: 'dev.user@mootd.local',
        name: 'MOOTD Dev User',
        avatarUrl: 'https://api.dicebear.com/9.x/initials/svg?seed=MD',
      },
      mode: 'mock',
    };
  }

  async signInWithGoogle(params: GoogleOAuthParams): Promise<AuthSession> {
    await this.delay();
    // In mock mode, return a placeholder session using the access token directly.
    // The API implementation verifies with Google and returns real user info.
    return {
      accessToken: params.accessToken,
      expiresAt: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
      user: {
        id: 'user_mock_google',
        email: 'mock.google@mootd.local',
        name: 'Mock Google User',
      },
      mode: 'mock',
    };
  }

  async refresh(_refreshToken: string): Promise<AuthSession> {
    await this.delay();
    return {
      accessToken: 'mock_refreshed_token',
      refreshToken: 'mock_refresh_token_new',
      expiresAt: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
      user: {
        id: 'user_mock_001',
        email: 'dev.user@mootd.local',
        name: 'MOOTD Dev User',
        avatarUrl: 'https://api.dicebear.com/9.x/initials/svg?seed=MD',
      },
      mode: 'mock',
    };
  }

  async logout(_refreshToken: string): Promise<void> {
    await this.delay(200);
  }
}
