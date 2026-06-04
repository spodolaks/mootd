import { useEffect, useCallback } from 'react';
import { useRouter } from 'expo-router';
import { WelcomeScreen } from '@/src/screens';
import { useAuthStore } from '@/src/store';
import { useGoogleAuth } from '@/src/hooks';
import { wardrobeRepository } from '@/src/data/repositories';
import { apiClient } from '@/src/data/api/client';

export default function Index() {
  const router = useRouter();
  const signInWithGoogle = useAuthStore(state => state.signInWithGoogle);
  const isAuthenticated = useAuthStore(state => state.isAuthenticated);
  const isLoading = useAuthStore(state => state.isLoading);
  const error = useAuthStore(state => state.error);

  /**
   * Route the signed-in user onward. A user with no profile gender is
   * sent through the onboarding gender step first — gender drives
   * which archetype-default fillers their moodboards mix in — then
   * the usual empty-wardrobe → build-wardrobe split.
   */
  const routeAfterAuth = useCallback(async () => {
    try {
      const profile = await apiClient.get<{ gender?: string }>('/v1/user/profile');
      if (!profile.gender) {
        router.replace('/onboarding-gender');
        return;
      }
    } catch {
      // Profile fetch failed (offline, or a mock user with no stored
      // doc) — skip the gender gate rather than block the user.
    }
    try {
      const { items } = await wardrobeRepository.getItems();
      router.replace(items.length === 0 ? '/build-wardrobe' : '/(main)/moodboard');
    } catch {
      // If the wardrobe check fails, proceed to the main screen.
      router.replace('/(main)/moodboard');
    }
  }, [router]);

  // If the session was already restored (e.g. browser refresh with valid
  // localStorage token), skip the login screen and go straight to the app.
  useEffect(() => {
    if (!isAuthenticated) return;
    void routeAfterAuth();
  }, [isAuthenticated, routeAfterAuth]);

  const { isReady, response, signIn, redirectUri } = useGoogleAuth();

  // Debug: log the redirect URI so we can register it in Google Console
  useEffect(() => {
    if (redirectUri) {
      console.log('[OAuth] Redirect URI:', redirectUri);
    }
  }, [redirectUri]);

  /**
   * When the OAuth response arrives with a token, pass it to the auth store.
   * The backend verifies the token with Google and returns the verified user profile.
   */
  const handleAuthResponse = useCallback(async () => {
    if (response?.type !== 'success') return;

    const accessToken = response.authentication?.accessToken;
    if (!accessToken) return;

    try {
      const isSignedIn = await signInWithGoogle({ accessToken });
      if (isSignedIn) {
        await routeAfterAuth();
      }
    } catch {
      // Error is handled by the auth store
    }
  }, [response, signInWithGoogle, routeAfterAuth]);

  useEffect(() => {
    void handleAuthResponse();
  }, [handleAuthResponse]);

  const handleGoogleSignIn = () => {
    if (isReady) {
      void signIn();
    }
  };

  return (
    <WelcomeScreen onGoogleSignIn={handleGoogleSignIn} isLoading={isLoading} errorMessage={error} />
  );
}
