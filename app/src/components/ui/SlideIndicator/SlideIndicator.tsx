import React from 'react';
import { View } from 'react-native';
import { labels, fills } from '../../../theme/colors';
import { spacing } from '../../../theme/spacing';
import { useColorScheme } from '@/src/hooks';
import type { SlideIndicatorProps } from './types';
import { getStyles } from './styles';

export const SlideIndicator: React.FC<SlideIndicatorProps> = ({
  totalDots,
  activeIndex,
  style,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles();

  const activeColor = labels.primary[colorScheme];
  const inactiveColor = fills.secondary[colorScheme];

  return (
    <View style={[styles.container, style]}>
      {Array.from({ length: totalDots }).map((_, index) => (
        <View
          key={index}
          style={[
            styles.dot,
            {
              backgroundColor: index === activeIndex ? activeColor : inactiveColor,
              marginLeft: index === 0 ? 0 : spacing.md - spacing.sm,
            },
          ]}
        />
      ))}
    </View>
  );
};
