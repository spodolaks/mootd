import React from 'react';
import { View, Text } from 'react-native';
import { Icon } from '../../icons';
import { getStyles } from './styles';
import { useColorScheme } from '@/src/hooks';
import { labels } from '../../../theme/colors';
import type { ToastProps } from './types';

export const Toast: React.FC<ToastProps> = ({ title, icon = 'plus', style, ...props }) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);

  const iconColor = labels.primary[colorScheme];

  return (
    <View style={[styles.container, style]} {...props}>
      <View style={styles.contentContainer}>
        {icon && (
          <View style={styles.iconContainer}>
            <Icon name={icon} size={24} color={iconColor} />
          </View>
        )}
        <Text style={styles.title}>{title}</Text>
      </View>
    </View>
  );
};
