import { Platform } from 'react-native';
import * as WebBrowser from 'expo-web-browser';
import * as Google from 'expo-auth-session/providers/google';
import * as AuthSession from 'expo-auth-session';

// Complete the auth session when the redirect comes back on web
WebBrowser.maybeCompleteAuthSession();

const WEB_CLIENT_ID =
  '991290253393-eompo9m0q8up56n7iabg30tn62lkd5h2.apps.googleusercontent.com';

export interface GoogleUserInfo {
  sub: string; // Google user ID
  email: string;
  name: string;
  picture?: string;
}

/**
 * Hook that configures Google OAuth via the dedicated
 * `expo-auth-session/providers/google` provider.
 *
 * This provider automatically handles:
 *  - The correct Google discovery document
 *  - Platform-specific redirect URIs
 *  - Scopes and response type
 */
export function useGoogleAuth() {
  // On web, explicitly build the redirect URI so it matches what's in Google Console.
  // Linking.createURL may add a trailing slash or path — we need an exact match.
  const redirectUri =
    Platform.OS === 'web'
      ? `${window.location.origin}`
      : AuthSession.makeRedirectUri({ scheme: 'mootdreactnative' });

  const [request, response, promptAsync] = Google.useAuthRequest({
    webClientId: WEB_CLIENT_ID,
    redirectUri,
    // Add iosClientId / androidClientId here later for native builds
    scopes: ['openid', 'profile', 'email'],
  });

  return {
    /** Whether the auth request has finished loading and is ready to fire */
    isReady: !!request,
    /** The raw response from the auth flow (null until user completes) */
    response,
    /** Kick off the Google sign-in prompt */
    signIn: () => promptAsync(),
    /** The redirect URI (useful for debugging) */
    redirectUri: request?.redirectUri,
  };
}

/**
 * Fetch Google user profile using the access token from the OAuth response.
 */
export async function fetchGoogleUserInfo(
  accessToken: string,
): Promise<GoogleUserInfo> {
  const res = await fetch('https://openidconnect.googleapis.com/v1/userinfo', {
    headers: { Authorization: `Bearer ${accessToken}` },
  });

  if (!res.ok) {
    throw new Error(`Failed to fetch user info (${res.status})`);
  }

  return res.json() as Promise<GoogleUserInfo>;
}
