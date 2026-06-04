import { Platform } from 'react-native';
import * as WebBrowser from 'expo-web-browser';
import * as Google from 'expo-auth-session/providers/google';

// Complete the auth session when the redirect comes back on web
WebBrowser.maybeCompleteAuthSession();

const WEB_CLIENT_ID = '991290253393-eompo9m0q8up56n7iabg30tn62lkd5h2.apps.googleusercontent.com';

// iOS OAuth client ID. Google validates this at mount on iOS and throws
// if missing; when the env var isn't populated we fall back to the web
// ID as a harmless placeholder so the app still boots and mock-login
// remains reachable. Real iOS sign-in needs EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID
// set + the matching iOS client configured in Google Cloud Console with
// the bundle ID that matches app.json's ios.bundleIdentifier.
const IOS_CLIENT_ID = process.env.EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID || WEB_CLIENT_ID;

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
  // Redirect URI handling per platform:
  //
  //   web: we fix it to the current origin so it matches what's registered
  //        in the Google Console. makeRedirectUri() sometimes adds a
  //        trailing slash or path, and Google compares strings exactly.
  //
  //   iOS/Android: we let the Google provider auto-derive the URI from
  //        iosClientId / androidClientId. Google's iOS OAuth clients
  //        only accept the reversed-client-ID scheme
  //        (com.googleusercontent.apps.<id>:/oauth2redirect/google) —
  //        passing a custom app-scheme URI here triggers 400
  //        invalid_request because the redirect doesn't match the client
  //        type. Leaving redirectUri undefined lets the provider pick
  //        the correct one.
  const redirectUri = Platform.OS === 'web' ? window.location.origin : undefined;

  const [request, response, promptAsync] = Google.useAuthRequest({
    webClientId: WEB_CLIENT_ID,
    iosClientId: IOS_CLIENT_ID,
    ...(redirectUri ? { redirectUri } : {}),
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
export async function fetchGoogleUserInfo(accessToken: string): Promise<GoogleUserInfo> {
  const res = await fetch('https://openidconnect.googleapis.com/v1/userinfo', {
    headers: { Authorization: `Bearer ${accessToken}` },
  });

  if (!res.ok) {
    throw new Error(`Failed to fetch user info (${res.status})`);
  }

  return res.json() as Promise<GoogleUserInfo>;
}
