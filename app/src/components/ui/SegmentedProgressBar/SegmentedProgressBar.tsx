import React from 'react';
import { View } from 'react-native';
import { LinearGradient } from 'expo-linear-gradient';
import { useColorScheme } from '@/src/hooks';
import { grays, fills, backgrounds } from '../../../theme/colors';
import { styles } from './styles';
import type { SegmentedProgressBarProps } from './types';

export const SegmentedProgressBar: React.FC<SegmentedProgressBarProps> = ({
  totalSegments,
  currentSegment,
  style,
  withFade = false,
}) => {
  const colorScheme = useColorScheme() ?? 'light';

  const activeColor = grays.black[colorScheme];
  const inactiveColor = fills.secondary[colorScheme];
  const backgroundColor = backgrounds.primary[colorScheme];

  return (
    <View style={[styles.wrapper, withFade && styles.wrapperWithFade]}>
      <View style={[styles.container, style]}>
        {Array.from({ length: totalSegments }).map((_, index) => (
          <View
            key={index}
            style={[
              styles.segment,
              {
                backgroundColor: index <= currentSegment ? activeColor : inactiveColor,
              },
            ]}
          />
        ))}
      </View>
      {withFade && <LinearGradient colors={[backgroundColor, 'transparent']} style={styles.fade} />}
    </View>
  );
};
