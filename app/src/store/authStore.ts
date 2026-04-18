import { create } from 'zustand';
import * as SecureStore from 'expo-secure-store';
import { Platform } from 'react-native';
import { authRepository } from '@/src/data';
import { setAuthToken } from '@/src/data/api/client';
import type { AuthSession, AuthUser, GoogleOAuthParams } from '@/src/domain';
import { usePreferencesStore } from './preferencesStore';

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
    } catch { /* ignore quota errors */ }
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
    } catch { /* ignore */ }
    return;
  }
  await SecureStore.deleteItemAsync(SECURE_TOKEN_KEY);
  await SecureStore.deleteItemAsync(SECURE_REFRESH_KEY);
  await SecureStore.deleteItemAsync(SECURE_SESSION_KEY);
}

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

export const useAuthStore = create<AuthState>((set) => ({
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
      return true;
    } catch (error) {
      const message =
        error instanceof Error ? error.message : 'Sign-in failed';
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
      return true;
    } catch (error) {
      const message =
        error instanceof Error ? error.message : 'Google sign-in failed';
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

      // Check token expiry before restoring.
      if (new Date(session.expiresAt) <= new Date()) {
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
    // Revoke the refresh token server-side first so a lost device can't keep
    // minting new access tokens. We do NOT await this call — network failures
    // must not block local sign-out, and the user expects an instant UI.
    const refreshToken = useAuthStore.getState().session?.refreshToken;
    if (refreshToken) {
      void authRepository.logout(refreshToken).catch(() => { /* already swallowed in repo */ });
    }

    setAuthToken(null);
    await clearTokenSecurely().catch((err) => {
      console.warn('[Auth] Failed to clear secure token:', err);
    });
    set({
      user: null,
      session: null,
      isAuthenticated: false,
      error: null,
    });
  },

  clearError: () => set({ error: null }),
}));
