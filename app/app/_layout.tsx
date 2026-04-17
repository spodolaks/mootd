import { DarkTheme, DefaultTheme, ThemeProvider } from '@react-navigation/native';
import { useFonts } from 'expo-font';
import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { StatusBar } from 'expo-status-bar';
import { Component, useEffect } from 'react';
import type { ReactNode, ErrorInfo } from 'react';
import { Platform, View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import 'react-native-reanimated';

import { ColorSchemeProvider, useColorScheme } from '@/src/hooks';
import { useAuthStore } from '@/src/store';

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

function RootLayoutContent() {
  const colorScheme = useColorScheme();

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
