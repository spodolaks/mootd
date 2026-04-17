import React, { useEffect, useRef } from 'react';
import { Animated, Easing } from 'react-native';
import Svg, { Circle } from 'react-native-svg';
import { useColorScheme } from '@/src/hooks';
import { labels } from '@/src/theme/colors';
import type { LoadingSpinnerProps } from './types';

export const LoadingSpinner: React.FC<LoadingSpinnerProps> = ({
  size = 28,
  style,
  color,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const spinValue = useRef(new Animated.Value(0)).current;

  const spinnerColor = color ?? labels.tertiary[colorScheme];

  // Circle dimensions matching Figma design
  const strokeWidth = 2;
  const radius = (size - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;
  // Show ~75% of the circle
  const arcLength = circumference * 0.75;

  useEffect(() => {
    const spinAnimation = Animated.loop(
      Animated.timing(spinValue, {
        toValue: 1,
        duration: 1000,
        easing: Easing.linear,
        useNativeDriver: true,
      })
    );
    spinAnimation.start();

    return () => {
      spinAnimation.stop();
    };
  }, [spinValue]);

  const spin = spinValue.interpolate({
    inputRange: [0, 1],
    outputRange: ['0deg', '360deg'],
  });

  return (
    <Animated.View style={[{ width: size, height: size, transform: [{ rotate: spin }] }, style]}>
      <Svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        <Circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          stroke={spinnerColor}
          strokeWidth={strokeWidth}
          strokeLinecap="round"
          strokeDasharray={`${arcLength} ${circumference}`}
          fill="none"
        />
      </Svg>
    </Animated.View>
  );
};
