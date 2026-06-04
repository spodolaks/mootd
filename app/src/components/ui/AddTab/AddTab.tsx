import React from 'react';
import { View, Text } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { Icon } from '../../icons';
import { labels } from '../../../theme/colors';
import { getStyles } from './styles';
import type { AddTabProps } from './types';

export const AddTab: React.FC<AddTabProps> = ({
  label = 'Label',
  icon = 'plus',
  disabled,
  selected = false,
  style,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme, selected);
  const iconColor = labels.primary[colorScheme];

  return (
    <View style={[styles.container, disabled && { opacity: 0.4 }, style]} pointerEvents="none">
      <Icon name={icon} size={24} color={iconColor} />
      <Text style={styles.label} numberOfLines={1}>
        {label}
      </Text>
    </View>
  );
};
