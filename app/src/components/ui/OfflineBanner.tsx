import React from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useConnectivity } from '@/src/hooks/useConnectivity';
import { useColorScheme } from '@/src/hooks';
import { typography } from '@/src/theme/typography';

/**
 * Sticky non-blocking banner that surfaces when the device is
 * offline (mootd#48). Renders nothing while connected — no
 * layout shift either way.
 *
 * Mounted once at the root so every screen sees the same
 * status; pairs with the `useConnectivity` hook that callers
 * use to gate polling loops while offline (poll loops on
 * detection / outfit job status pause until reconnected).
 */
export const OfflineBanner: React.FC = () => {
  const { isConnected } = useConnectivity();
  const colorScheme = useColorScheme() ?? 'light';

  if (isConnected) return null;

  // Amber on dark + warm-red text on light. Subtle enough to
  // read as info, not alarming enough to feel like a crash.
  const bg = colorScheme === 'dark' ? '#3a2810' : '#fef3c7';
  const fg = colorScheme === 'dark' ? '#f8d36a' : '#92400e';

  return (
    <SafeAreaView edges={['top']} style={[styles.wrap, { backgroundColor: bg }]}>
      <View style={styles.row}>
        <Text style={[styles.text, { color: fg }]} accessibilityLiveRegion="polite">
          No internet connection — some features are unavailable.
        </Text>
      </View>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  wrap: {
    width: '100%',
  },
  row: {
    paddingHorizontal: 16,
    paddingVertical: 8,
    alignItems: 'center',
  },
  text: {
    ...typography.footnote.regular,
  },
});
