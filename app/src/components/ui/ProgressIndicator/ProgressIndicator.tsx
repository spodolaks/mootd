import React from 'react';
import { View } from 'react-native';
import { Text } from '../Text';
import { Icon } from '../../icons';
import { labels, fills } from '../../../theme/colors';
import { getStyles } from './styles';
import { useColorScheme } from '@/src/hooks';
import type { ProgressIndicatorProps } from './types';

export const ProgressIndicator: React.FC<ProgressIndicatorProps> = ({
  steps,
  activeIndex = 0,
  onStepPress,
  style,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);

  const iconColor = labels.primary[colorScheme];
  const activeStepBackground = fills.secondary[colorScheme];

  return (
    <View style={[styles.container, style]}>
      {steps.map((step, index) => {
        return (
          <View
            key={step.id}
            style={styles.stepContainer}
            onTouchEnd={() => onStepPress?.(index, step)}>
            <View style={[styles.stepIndicator, { backgroundColor: activeStepBackground }]}>
              <Icon name={step.icon ?? 'plus'} size={16} color={iconColor} />
            </View>
            <Text variant="caption1" style={styles.label}>
              {step.label}
            </Text>
          </View>
        );
      })}
    </View>
  );
};
