import React, { useEffect } from 'react';
import { ViewStyle } from 'react-native';
import Animated, {
  cancelAnimation,
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withTiming,
} from 'react-native-reanimated';
import { useColorScheme } from '@/src/hooks';
import { fills } from '@/src/theme/colors';

/**
 * Skeleton placeholder (mootd#50). A flat rectangle that pulses
 * its opacity in a 1.2s loop so users perceive the screen as
 * "loading content shaped like real content" rather than an
 * indeterminate spinner.
 *
 * Usage:
 *   <Skeleton style={{ width: 160, height: 160, borderRadius: 12 }} />
 *
 * Compose specific skeletons (a wardrobe card, a moodboard card)
 * by wrapping a few <Skeleton /> rectangles in the layout the
 * real component uses, so the row dimensions match.
 */
export const Skeleton: React.FC<{ style?: ViewStyle }> = ({ style }) => {
  const colorScheme = useColorScheme() ?? 'light';
  const opacity = useSharedValue(0.6);

  useEffect(() => {
    opacity.value = withRepeat(
      withTiming(0.25, { duration: 600 }),
      -1, // infinite
      true // reverse → 0.6 ↔ 0.25 ping-pong
    );
    return () => {
      cancelAnimation(opacity);
    };
  }, [opacity]);

  const animatedStyle = useAnimatedStyle(() => ({
    opacity: opacity.value,
  }));

  return (
    <Animated.View
      style={[
        {
          backgroundColor: fills.tertiary[colorScheme],
          borderRadius: 8,
        },
        style,
        animatedStyle,
      ]}
      accessibilityElementsHidden
      importantForAccessibility="no"
    />
  );
};
