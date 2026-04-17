import React from 'react';
import { ActivityIndicator, StyleSheet, View } from 'react-native';
import { Text } from './Text';

export interface LoadingOverlayProps {
  /** Message shown below the spinner. Defaults to "Loading…" */
  message?: string;
}

export const LoadingOverlay: React.FC<LoadingOverlayProps> = ({
  message = 'Loading…',
}) => (
  <View style={styles.overlay}>
    <View style={styles.box}>
      <ActivityIndicator size="large" color="#007AFF" />
      <Text variant="subheadline" weight="semiBold" style={styles.label}>
        {message}
      </Text>
    </View>
  </View>
);

const styles = StyleSheet.create({
  overlay: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'rgba(0, 0, 0, 0.45)',
    justifyContent: 'center',
    alignItems: 'center',
    zIndex: 100,
  },
  box: {
    backgroundColor: 'rgba(255, 255, 255, 0.95)',
    borderRadius: 16,
    padding: 32,
    alignItems: 'center',
    gap: 16,
  },
  label: {
    textAlign: 'center',
  },
});
