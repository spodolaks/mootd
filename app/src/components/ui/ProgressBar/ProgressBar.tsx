import React from 'react';
import { View } from 'react-native';
import { labels, fills } from '../../../theme/colors';
import { useColorScheme } from '@/src/hooks';
import type { ProgressBarProps } from './types';
import { getStyles } from './styles';

export const ProgressBar: React.FC<ProgressBarProps> = ({ progress, height = 6, style }) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles();

  const trackColor = fills.secondary[colorScheme];
  const progressColor = labels.primary[colorScheme];

  // Clamp progress between 0 and 1
  const clampedProgress = Math.min(1, Math.max(0, progress));

  return (
    <View
      style={[
        styles.container,
        {
          height,
          borderRadius: height / 2,
          backgroundColor: trackColor,
        },
        style,
      ]}>
      <View
        style={[
          styles.progress,
          {
            width: `${clampedProgress * 100}%`,
            height,
            borderRadius: height / 2,
            backgroundColor: progressColor,
          },
        ]}
      />
    </View>
  );
};
