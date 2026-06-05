import React, { useEffect, useRef } from 'react';
import { Animated, Pressable, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Text } from './Text';
import { useUIStore } from '@/src/store';
import { useColorScheme } from '@/src/hooks';
import { accents, backgrounds, labels, separators } from '@/src/theme/colors';
import { spacing, radius } from '@/src/theme';
import { typography } from '@/src/theme/typography';

type ToastType = 'success' | 'error' | 'info';
type Scheme = 'light' | 'dark';

const statusColor = (type: ToastType, scheme: Scheme): string => {
  switch (type) {
    case 'success':
      return accents.green[scheme];
    case 'error':
      return accents.red[scheme];
    default:
      return accents.blue[scheme];
  }
};

interface ToastRowProps {
  id: string;
  message: string;
  type: ToastType;
  scheme: Scheme;
  onDismiss: (id: string) => void;
}

const ToastRow: React.FC<ToastRowProps> = ({ id, message, type, scheme, onDismiss }) => {
  const anim = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    Animated.timing(anim, {
      toValue: 1,
      duration: 220,
      useNativeDriver: true,
    }).start();
  }, [anim]);

  return (
    <Animated.View
      style={{
        opacity: anim,
        transform: [
          { translateY: anim.interpolate({ inputRange: [0, 1], outputRange: [-12, 0] }) },
        ],
      }}>
      <Pressable
        onPress={() => onDismiss(id)}
        accessibilityRole="alert"
        accessibilityLabel={message}
        testID={`toast-${type}`}
        style={[
          styles.row,
          {
            backgroundColor: backgrounds.secondary[scheme],
            borderColor: separators.secondary[scheme],
          },
        ]}>
        <View style={[styles.dot, { backgroundColor: statusColor(type, scheme) }]} />
        <Text style={[styles.message, { color: labels.primary[scheme] }]}>{message}</Text>
      </Pressable>
    </Animated.View>
  );
};

/**
 * Renders the global toast queue from `uiStore`. Mounted once at the
 * root layout so any screen calling `showToast()` produces visible,
 * accessible feedback. The store auto-dismisses each toast after 3s;
 * tapping a toast dismisses it early.
 */
export const ToastHost: React.FC = () => {
  const insets = useSafeAreaInsets();
  const scheme = useColorScheme() ?? 'light';
  const toasts = useUIStore(s => s.toasts);
  const dismissToast = useUIStore(s => s.dismissToast);

  if (toasts.length === 0) return null;

  return (
    <View
      pointerEvents="box-none"
      accessibilityLiveRegion="polite"
      style={[styles.host, { top: insets.top + spacing.sm }]}>
      {toasts.map(t => (
        <ToastRow
          key={t.id}
          id={t.id}
          message={t.message}
          type={t.type}
          scheme={scheme}
          onDismiss={dismissToast}
        />
      ))}
    </View>
  );
};

const styles = StyleSheet.create({
  host: {
    position: 'absolute',
    left: spacing.md,
    right: spacing.md,
    zIndex: 1000,
    gap: spacing.sm,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    borderRadius: radius.lg,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm + 2,
    shadowColor: '#000000',
    shadowOffset: { width: 0, height: 4 },
    shadowOpacity: 0.12,
    shadowRadius: 12,
    elevation: 4,
  },
  dot: {
    width: 8,
    height: 8,
    borderRadius: 4,
    marginRight: spacing.sm,
  },
  message: {
    ...typography.subheadline.regular,
    flex: 1,
  },
});
