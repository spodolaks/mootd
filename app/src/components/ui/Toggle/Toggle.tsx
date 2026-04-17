import React, { useEffect, useRef } from 'react';
import { Pressable, View, Animated } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { grays, fills } from '../../../theme/colors';
import type { ToggleProps } from './types';
import { getStyles } from './styles';

// Animation constants (must match styles.ts dimensions)
const TRACK_WIDTH = 64;
const THUMB_WIDTH = 39;
const THUMB_MARGIN = 2;
const THUMB_TRAVEL = TRACK_WIDTH - THUMB_WIDTH - THUMB_MARGIN * 2;

export const Toggle: React.FC<ToggleProps> = ({
  value,
  onValueChange,
  disabled = false,
  style,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles();
  const animatedValue = useRef(new Animated.Value(value ? 1 : 0)).current;

  useEffect(() => {
    Animated.timing(animatedValue, {
      toValue: value ? 1 : 0,
      duration: 200,
      useNativeDriver: false,
    }).start();
  }, [value, animatedValue]);

  const handlePress = () => {
    if (!disabled) {
      onValueChange(!value);
    }
  };

  // Interpolate thumb position
  const thumbTranslateX = animatedValue.interpolate({
    inputRange: [0, 1],
    outputRange: [THUMB_MARGIN, THUMB_MARGIN + THUMB_TRAVEL],
  });

  // Colors based on state and theme
  const getTrackColor = () => {
    if (disabled) {
      return fills.tertiary[colorScheme];
    }
    if (value) {
      return colorScheme === 'light' ? grays.black.light : grays.white.light;
    }
    return fills.primary[colorScheme];
  };

  const getThumbColor = () => {
    if (value) {
      return colorScheme === 'light' ? grays.white.light : grays.black.light;
    }
    return colorScheme === 'light' ? grays.white.light : grays.white.dark;
  };

  return (
    <Pressable
      onPress={handlePress}
      disabled={disabled}
      style={[styles.container, style]}>
      <View
        style={[
          styles.track,
          {
            backgroundColor: getTrackColor(),
            opacity: disabled ? 0.5 : 1,
          },
        ]}>
        <Animated.View
          style={[
            styles.thumb,
            {
              backgroundColor: getThumbColor(),
              transform: [{ translateX: thumbTranslateX }],
            },
          ]}
        />
      </View>
    </Pressable>
  );
};
