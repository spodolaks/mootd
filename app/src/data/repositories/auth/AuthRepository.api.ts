import type { AuthSession, GoogleOAuthParams, IAuthRepository } from '@/src/domain';
import { apiClient, setAuthToken } from '@/src/data/api/client';

interface AuthAPIResponse {
  accessToken: string;
  refreshToken?: string;
  expiresAt: string;
  user: {
    id: string;
    email: string;
    name: string;
    avatarUrl?: string;
  };
  mode: 'mock' | 'api';
}

export class ApiAuthRepository implements IAuthRepository {
  async signInWithGoogleMock(): Promise<AuthSession> {
    const response = await apiClient.post<AuthAPIResponse>('/v1/auth/mock-login', {
      provider: 'google',
    });

    setAuthToken(response.accessToken);

    return {
      accessToken: response.accessToken,
      refreshToken: response.refreshToken,
      expiresAt: response.expiresAt,
      user: response.user,
      mode: response.mode === 'api' ? 'api' : 'mock',
    };
  }

  /**
   * Exchange the Google OAuth access token with the backend.
   * The backend verifies the token with Google directly and returns a signed
   * mootd JWT along with the verified user profile.
   *
   * Endpoint: POST /v1/auth/google
   * Body: { accessToken }
   * Response: { accessToken (mootd JWT), expiresAt, user, mode }
   */
  async signInWithGoogle(params: GoogleOAuthParams): Promise<AuthSession> {
    const response = await apiClient.post<AuthAPIResponse>('/v1/auth/google', {
      accessToken: params.accessToken,
    });

    setAuthToken(response.accessToken);

    return {
      accessToken: response.accessToken,
      refreshToken: response.refreshToken,
      expiresAt: response.expiresAt,
      user: response.user,
      mode: response.mode === 'api' ? 'api' : 'mock',
    };
  }

  async refresh(refreshToken: string): Promise<AuthSession> {
    const response = await apiClient.post<{
      accessToken: string;
      refreshToken: string;
      expiresAt: string;
      user: { id: string; email: string; name: string; avatarUrl?: string };
    }>('/v1/auth/refresh', { refreshToken });

    setAuthToken(response.accessToken);

    return {
      accessToken: response.accessToken,
      refreshToken: response.refreshToken,
      expiresAt: response.expiresAt,
      user: response.user,
      mode: 'api',
    };
  }

  async logout(refreshToken: string): Promise<void> {
    // Revoke on the server, but never let a network failure block local sign-out.
    try {
      await apiClient.post<unknown>('/v1/auth/logout', { refreshToken });
    } catch {
      // Swallow — caller clears local state regardless.
    }
  }
}
