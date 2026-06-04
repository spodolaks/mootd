import React from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, labels, overlays } from '@/src/theme/colors';
import { radius } from '@/src/theme/radius';
import { spacing } from '@/src/theme/spacing';
import { Text } from './Text';

export interface LoadingOverlayProps {
  /** Message shown below the spinner. Defaults to "Loading…" */
  message?: string;
}

export const LoadingOverlay: React.FC<LoadingOverlayProps> = ({ message = 'Loading…' }) => {
  const colorScheme = useColorScheme() ?? 'light';

  return (
    <View
      style={[styles.overlay, { backgroundColor: overlays.default[colorScheme] }]}
      accessibilityRole="progressbar"
      accessibilityLabel={message}>
      <View style={[styles.box, { backgroundColor: backgrounds.secondary[colorScheme] }]}>
        <ActivityIndicator size="large" color={labels.primary[colorScheme]} />
        <Text variant="subheadline" weight="semiBold" style={styles.label}>
          {message}
        </Text>
      </View>
    </View>
  );
};

const styles = StyleSheet.create({
  overlay: {
    ...StyleSheet.absoluteFillObject,
    justifyContent: 'center',
    alignItems: 'center',
    zIndex: 100,
  },
  box: {
    borderRadius: radius.xl,
    padding: spacing.xl,
    alignItems: 'center',
    gap: spacing.md,
  },
  label: {
    textAlign: 'center',
  },
});
