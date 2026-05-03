import { DarkTheme, DefaultTheme, ThemeProvider } from '@react-navigation/native';
import { useFonts } from 'expo-font';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { Component, useEffect, useRef } from 'react';
import type { ReactNode, ErrorInfo } from 'react';
import Constants from 'expo-constants';
import { AppState, AppStateStatus, Platform, View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import 'react-native-reanimated';

import { ColorSchemeProvider, useColorScheme } from '@/src/hooks';
import { useAuthStore } from '@/src/store';
import * as events from '@/src/lib/events';
import { getApiBaseURL } from '@/src/data/api/client';

// ─── Error Boundary ──────────────────────────────────────────────────────────

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

class AppErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Replace with a proper logger when one is added (e.g. Sentry)
    console.error('[AppErrorBoundary]', error, info.componentStack);
  }

  render() {
    if (this.state.hasError) {
      return (
        <View style={errorStyles.container}>
          <Text style={errorStyles.title}>Something went wrong</Text>
          <Text style={errorStyles.message}>
            {this.state.error?.message ?? 'An unexpected error occurred.'}
          </Text>
          <TouchableOpacity
            style={errorStyles.button}
            onPress={() => this.setState({ hasError: false, error: null })}
          >
            <Text style={errorStyles.buttonText}>Try again</Text>
          </TouchableOpacity>
        </View>
      );
    }
    return this.props.children;
  }
}

const errorStyles = StyleSheet.create({
  container: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    padding: 24,
    backgroundColor: '#F2F2F7',
  },
  title: {
    fontSize: 20,
    fontWeight: '600',
    color: '#000000',
    marginBottom: 8,
  },
  message: {
    fontSize: 14,
    color: 'rgba(60,60,67,0.6)',
    textAlign: 'center',
    marginBottom: 24,
  },
  button: {
    paddingHorizontal: 24,
    paddingVertical: 12,
    borderRadius: 12,
    backgroundColor: '#000000',
  },
  buttonText: {
    color: '#FFFFFF',
    fontWeight: '600',
    fontSize: 16,
  },
});

// Prevent the splash screen from auto-hiding before fonts are loaded
SplashScreen.preventAutoHideAsync();

export const unstable_settings = {
  initialRouteName: 'index',
};

// SDK lifecycle (P2-01 / mootd-admin#18 + P2-03 / mootd-admin#20).
//
// Behaviour:
//   - On first foreground after launch: emit `app_opened`
//     (sessionType: cold).
//   - On every subsequent foreground after >30s in background:
//     rotate sessionId, emit `session_end` for the previous
//     session, emit `app_opened` (sessionType: warm) for the new.
//   - On background: stash the timestamp + a snapshot of the
//     session's screensVisited / featuresUsed so the next
//     active->background transition can compute durations.
//
// Why hooked here instead of in the SDK itself: the SDK stays
// platform-agnostic (no `react-native` import). The lifecycle
// driver is the Expo entry point's responsibility.
const BACKGROUND_THRESHOLD_MS = 30_000;

function useEventsLifecycle(authToken: string | null): void {
  const lastBackgroundedAt = useRef<number | null>(null);
  const sessionStartAt = useRef<number>(Date.now());
  const lastAppState = useRef<AppStateStatus>(AppState.currentState);

  // Boot the SDK once, before any emit. Idempotent so a
  // hot-reload doesn't double-start.
  useEffect(() => {
    void events.start({ apiBaseUrl: getApiBaseURL() });
    return () => events.stop();
  }, []);

  // Forward the auth token. Bound to the auth store via the
  // caller; on login the store fires this hook with a new
  // token and the SDK flushes any anonymous-queued events.
  useEffect(() => {
    events.setAuthToken(authToken);
  }, [authToken]);

  // Cold-launch app_opened + session_start. Fires once on
  // mount. session_start carries platform; the analyses use
  // it as the cohort anchor (P2-05 retention reads
  // signed_up + session_start + session_heartbeat).
  useEffect(() => {
    const platform = Platform.OS as 'ios' | 'android' | 'web';
    const appVersion =
      (Constants?.expoConfig?.version as string | undefined) ?? '0.0.0';
    events.emit('app_opened', {
      platform,
      appVersion,
      sessionType: 'cold',
    });
    events.emit('session_start', { platform });
  }, []);

  // Heartbeat (P2-03 / mootd-admin#20). One every 60s while
  // foregrounded — matches the issue's "max 1/min" cap. Lets
  // the server reconstruct "session was alive at minute N"
  // even if session_end is dropped (force-close, OS kill).
  useEffect(() => {
    const heartbeat = setInterval(() => {
      if (lastAppState.current !== 'active') return;
      const elapsedSec = Math.floor(
        (Date.now() - sessionStartAt.current) / 1000,
      );
      events.emit('session_heartbeat', { elapsedSec });
    }, 60_000);
    return () => clearInterval(heartbeat);
  }, []);

  // AppState transitions: track background → foreground for
  // session lifecycle, foreground → background for stashing.
  useEffect(() => {
    const sub = AppState.addEventListener('change', (next) => {
      const prev = lastAppState.current;
      lastAppState.current = next;

      if (prev === 'active' && next !== 'active') {
        // Going to background.
        lastBackgroundedAt.current = Date.now();
        return;
      }

      if (next === 'active' && prev !== 'active') {
        // Coming to foreground.
        const since = lastBackgroundedAt.current;
        lastBackgroundedAt.current = null;
        if (since === null) return;

        const awayMs = Date.now() - since;
        if (awayMs > BACKGROUND_THRESHOLD_MS) {
          // Long-enough background → end session, start a new one.
          const elapsed = since - sessionStartAt.current;
          events.emit('session_end', {
            durationMs: Math.max(0, elapsed),
            // We don't currently track these client-side; ship
            // zero/empty + tighten in a follow-up if the analyses
            // ask for them.
            screensVisited: 0,
            featuresUsed: [],
          });
          events.rotateSessionId();
          sessionStartAt.current = Date.now();
          const platform = Platform.OS as 'ios' | 'android' | 'web';
          events.emit('app_opened', {
            platform,
            appVersion:
              (Constants?.expoConfig?.version as string | undefined) ?? '0.0.0',
            sessionType: 'warm',
          });
          events.emit('session_start', { platform });
          // Force a flush so the new session is visible to
          // analyses immediately.
          void events.flush();
        }
      }
    });
    return () => sub.remove();
  }, []);
}

function RootLayoutContent() {
  const colorScheme = useColorScheme();
  const session = useAuthStore((s) => s.session);
  useEventsLifecycle(session?.accessToken ?? null);

  return (
    <ThemeProvider value={colorScheme === 'dark' ? DarkTheme : DefaultTheme}>
      <Stack>
        <Stack.Screen name="index" options={{ headerShown: false }} />
        <Stack.Screen name="build-wardrobe" options={{ headerShown: false }} />
        <Stack.Screen name="detected-item" options={{ headerShown: false }} />
        <Stack.Screen name="trait-selection" options={{ headerShown: false }} />
        <Stack.Screen name="permissions" options={{ headerShown: false }} />
        <Stack.Screen name="loading" options={{ headerShown: false }} />
        <Stack.Screen name="moodboard" options={{ headerShown: false }} />
        <Stack.Screen name="(main)" options={{ headerShown: false }} />
        <Stack.Screen name="item-details" options={{ headerShown: false }} />
        <Stack.Screen name="preferences" options={{ headerShown: false }} />
      </Stack>
      <StatusBar style={colorScheme === 'dark' ? 'light' : 'dark'} />
    </ThemeProvider>
  );
}

export default function RootLayout() {
  const restoreSession = useAuthStore((state) => state.restoreSession);
  const sessionRestored = useAuthStore((state) => state.sessionRestored);

  // On web the TTF files are mobile-specific binaries that Chrome's OTS font
  // sanitizer rejects. Skip loading them on web and fall back to the system
  // sans-serif stack so the app is still fully functional for web testing.
  const [fontsLoaded] = useFonts(
    Platform.OS === 'web'
      ? {}
      : {
          'MontserratAlternates-Regular': require('../assets/fonts/MontserratAlternates-Regular.ttf'),
          'MontserratAlternates-SemiBold': require('../assets/fonts/MontserratAlternates-SemiBold.ttf'),
        }
  );

  useEffect(() => {
    void restoreSession();
  }, [restoreSession]);

  useEffect(() => {
    if (fontsLoaded) {
      SplashScreen.hideAsync();
    }
  }, [fontsLoaded]);

  // Wait for both fonts and session restore before rendering routes.
  // This prevents authenticated routes from firing API calls before the JWT
  // is loaded back into memory.
  if (!fontsLoaded || !sessionRestored) {
    return null;
  }

  return (
    <AppErrorBoundary>
      <ColorSchemeProvider>
        <RootLayoutContent />
      </ColorSchemeProvider>
    </AppErrorBoundary>
  );
}
