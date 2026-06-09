import { create } from 'zustand';
import * as SecureStore from 'expo-secure-store';
import { Platform } from 'react-native';
import { authRepository } from '@/src/data';
import { setAuthToken } from '@/src/data/api/client';
import * as events from '@/src/lib/events';
import type { AuthSession, AuthUser, GoogleOAuthParams } from '@/src/domain';
import { usePreferencesStore } from './preferencesStore';
import { useWardrobeStore } from './wardrobeStore';
import { useDetectionJobStore } from './detectionJobStore';

const SECURE_TOKEN_KEY = 'mootd.auth.token';
const SECURE_REFRESH_KEY = 'mootd.auth.refreshToken';
const SECURE_SESSION_KEY = 'mootd.auth.session';

/** Persist the mootd JWT securely (SecureStore on native, localStorage on web). */
async function saveTokenSecurely(token: string, session: AuthSession): Promise<void> {
  if (Platform.OS === 'web') {
    try {
      localStorage.setItem(SECURE_TOKEN_KEY, token);
      localStorage.setItem(SECURE_SESSION_KEY, JSON.stringify(session));
      if (session.refreshToken) {
        localStorage.setItem(SECURE_REFRESH_KEY, session.refreshToken);
      }
    } catch {
      /* ignore quota errors */
    }
    return;
  }
  await SecureStore.setItemAsync(SECURE_TOKEN_KEY, token);
  await SecureStore.setItemAsync(SECURE_SESSION_KEY, JSON.stringify(session));
  if (session.refreshToken) {
    await SecureStore.setItemAsync(SECURE_REFRESH_KEY, session.refreshToken);
  }
}

/** Remove the persisted token. */
async function clearTokenSecurely(): Promise<void> {
  if (Platform.OS === 'web') {
    try {
      localStorage.removeItem(SECURE_TOKEN_KEY);
      localStorage.removeItem(SECURE_REFRESH_KEY);
      localStorage.removeItem(SECURE_SESSION_KEY);
    } catch {
      /* ignore */
    }
    return;
  }
  await SecureStore.deleteItemAsync(SECURE_TOKEN_KEY);
  await SecureStore.deleteItemAsync(SECURE_REFRESH_KEY);
  await SecureStore.deleteItemAsync(SECURE_SESSION_KEY);
}

/**
 * Guards signOut against re-entrancy. A wave of requests that all 401 and all
 * fail to refresh would otherwise each call signOut → a storm of logout calls
 * and redundant state churn. First caller wins; the rest no-op (#98).
 */
let isSigningOut = false;

export interface AuthState {
  user: AuthUser | null;
  session: AuthSession | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  /** True once restoreSession has finished (regardless of whether a session was found). */
  sessionRestored: boolean;
  error: string | null;
  signInWithGoogleMock: () => Promise<boolean>;
  /** Real Google OAuth sign-in — pass the Google access token from the OAuth hook */
  signInWithGoogle: (params: GoogleOAuthParams) => Promise<boolean>;
  /** Restore a persisted session from SecureStore on app launch */
  restoreSession: () => Promise<void>;
  /** Refresh the access token using the stored refresh token */
  refreshSession: () => Promise<void>;
  signOut: () => Promise<void>;
  clearError: () => void;
}

/** Sync user profile data to the persisted preferences store. */
function syncToPreferences(user: AuthUser) {
  const prefs = usePreferencesStore.getState();
  if (user.email) prefs.setEmail(user.email);
  // Only set display name if user hasn't customised it yet
  if (!prefs.displayName && user.name) prefs.setDisplayName(user.name);
}

export const useAuthStore = create<AuthState>(set => ({
  user: null,
  session: null,
  isLoading: false,
  isAuthenticated: false,
  sessionRestored: false,
  error: null,

  signInWithGoogleMock: async () => {
    set({ isLoading: true, error: null });
    try {
      const session = await authRepository.signInWithGoogleMock();
      await saveTokenSecurely(session.accessToken, session);
      set({
        user: session.user,
        session,
        isAuthenticated: true,
      });
      syncToPreferences(session.user);
      // P2-01: emit before returning so the event lands on
      // whichever onLoginSuccess callback the caller routes
      // through. The first emit after login also flushes the
      // queued anonymous events (if any).
      events.emit('signed_in', { method: 'google' });
      return true;
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Sign-in failed';
      set({
        user: null,
        session: null,
        isAuthenticated: false,
        error: message,
      });
      return false;
    } finally {
      set({ isLoading: false });
    }
  },

  signInWithGoogle: async (params: GoogleOAuthParams) => {
    set({ isLoading: true, error: null });
    try {
      const session = await authRepository.signInWithGoogle(params);
      await saveTokenSecurely(session.accessToken, session);
      set({
        user: session.user,
        session,
        isAuthenticated: true,
      });
      syncToPreferences(session.user);
      events.emit('signed_in', { method: 'google' });
      return true;
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Google sign-in failed';
      set({
        user: null,
        session: null,
        isAuthenticated: false,
        error: message,
      });
      return false;
    } finally {
      set({ isLoading: false });
    }
  },

  restoreSession: async () => {
    try {
      let token: string | null;
      let rawSession: string | null;
      let refreshToken: string | null;

      if (Platform.OS === 'web') {
        token = localStorage.getItem(SECURE_TOKEN_KEY);
        rawSession = localStorage.getItem(SECURE_SESSION_KEY);
        refreshToken = localStorage.getItem(SECURE_REFRESH_KEY);
      } else {
        [token, rawSession, refreshToken] = await Promise.all([
          SecureStore.getItemAsync(SECURE_TOKEN_KEY),
          SecureStore.getItemAsync(SECURE_SESSION_KEY),
          SecureStore.getItemAsync(SECURE_REFRESH_KEY),
        ]);
      }
      if (!token || !rawSession) {
        set({ sessionRestored: true });
        return;
      }

      const session = JSON.parse(rawSession) as AuthSession;
      // Re-attach the refresh token from dedicated secure storage
      if (refreshToken) {
        session.refreshToken = refreshToken;
      }

      // The access token is short-lived (15 min); session.expiresAt is its
      // expiry, not the refresh token's. If it has expired but we still hold a
      // refresh token (valid for 30 days), mint a fresh access token instead of
      // forcing a full re-login — otherwise simply reopening the app the next
      // day always bounced the user back to the login screen (#146).
      if (new Date(session.expiresAt) <= new Date()) {
        if (refreshToken) {
          try {
            const newSession = await authRepository.refresh(refreshToken);
            await saveTokenSecurely(newSession.accessToken, newSession);
            setAuthToken(newSession.accessToken);
            set({
              user: newSession.user,
              session: newSession,
              isAuthenticated: true,
              sessionRestored: true,
            });
            syncToPreferences(newSession.user);
            return;
          } catch {
            // Refresh token rejected (expired/revoked) — fall through to a
            // clean signed-out state.
          }
        }
        await clearTokenSecurely();
        set({ sessionRestored: true });
        return;
      }

      setAuthToken(token);
      set({
        user: session.user,
        session,
        isAuthenticated: true,
        sessionRestored: true,
      });
      syncToPreferences(session.user);
    } catch {
      // Corrupted storage — clear and let user sign in again.
      await clearTokenSecurely();
      set({ sessionRestored: true });
    }
  },

  refreshSession: async () => {
    const currentSession = useAuthStore.getState().session;
    if (!currentSession?.refreshToken) {
      throw new Error('No refresh token available');
    }

    const newSession = await authRepository.refresh(currentSession.refreshToken);
    await saveTokenSecurely(newSession.accessToken, newSession);
    set({
      user: newSession.user,
      session: newSession,
      isAuthenticated: true,
    });
    syncToPreferences(newSession.user);
  },

  signOut: async () => {
    // Re-entrancy guard: dedup a storm of concurrent signOuts (#98).
    if (isSigningOut) return;
    isSigningOut = true;
    try {
      // P2-01: emit before clearing the token so the event flushes
      // with valid auth. The SDK keeps queueing post-signout (in
      // case of immediate sign-back-in), but those land on the
      // next user's session — anonymous queue gets the rest.
      events.emit('signed_out', {});
      void events.flush();

      // Revoke the refresh token server-side first so a lost device can't keep
      // minting new access tokens. We do NOT await this call — network failures
      // must not block local sign-out, and the user expects an instant UI.
      // Read the token before clearing auth state below, since that wipes it.
      const refreshToken = useAuthStore.getState().session?.refreshToken;
      if (refreshToken) {
        void authRepository.logout(refreshToken).catch(() => {
          /* already swallowed in repo */
        });
      }

      // #135: flip auth state to signed-out BEFORE the awaited
      // clearTokenSecurely() below. Previously this ran after the await, so a
      // fire-and-forget signOut() call site that immediately navigated would
      // hit index.tsx while isAuthenticated was still true and get bounced
      // back into (main). Clearing synchronously here guarantees any navigation
      // observes the signed-out state.
      setAuthToken(null);
      set({
        user: null,
        session: null,
        isAuthenticated: false,
        error: null,
      });

      // #148: wipe every other per-user store so a shared device doesn't leak
      // user A's data to user B. preferencesStore (persisted: display name,
      // email, creativity, theme…) resets to defaults; the in-memory
      // detectionJob + wardrobe wizard stores reset to empty. getState() is
      // used because this runs outside React.
      usePreferencesStore.getState().reset();
      useDetectionJobStore.getState().clear();
      useWardrobeStore.getState().reset();

      await clearTokenSecurely().catch(err => {
        console.warn('[Auth] Failed to clear secure token:', err);
      });
    } finally {
      isSigningOut = false;
    }
  },

  clearError: () => set({ error: null }),
}));
