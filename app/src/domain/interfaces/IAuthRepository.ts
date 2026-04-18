import type { AuthSession } from '../models/AuthSession';

export interface GoogleOAuthParams {
  /** The Google access token from expo-auth-session. The backend verifies this
   *  with Google directly and extracts user info — no profile fields are needed. */
  accessToken: string;
}

export interface IAuthRepository {
  signInWithGoogleMock: () => Promise<AuthSession>;
  /** Exchange a Google OAuth token + profile for an app session */
  signInWithGoogle: (params: GoogleOAuthParams) => Promise<AuthSession>;
  /** Exchange a refresh token for a new access + refresh token pair */
  refresh: (refreshToken: string) => Promise<AuthSession>;
  /** Revoke the refresh token server-side. Best-effort: never throws. */
  logout: (refreshToken: string) => Promise<void>;
}
