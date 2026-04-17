import React from 'react';
import { View, Text, Pressable } from 'react-native';
import { Icon } from '../../icons';
import { getStyles } from './styles';
import { useColorScheme } from '@/src/hooks';
import { labels } from '../../../theme/colors';
import type { InfoProps } from './types';

export const Info: React.FC<InfoProps> = ({ title, description, onClose, style, ...props }) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);

  const iconColor = labels.primary[colorScheme];

  return (
    <View style={[styles.container, style]} {...props}>
      <View style={styles.contentContainer}>
        <View style={styles.iconContainer}>
          <Icon name="info" size={24} color={iconColor} />
        </View>
        <View style={styles.textContainer}>
          <Text style={styles.title}>{title}</Text>
          {description && <Text style={styles.description}>{description}</Text>}
        </View>
      </View>
      {onClose && (
        <Pressable
          style={styles.closeButton}
          onPress={onClose}
          hitSlop={8}
          accessibilityRole="button"
          accessibilityLabel="Close">
          <Icon name="close" size={24} color={iconColor} />
        </Pressable>
      )}
    </View>
  );
};
