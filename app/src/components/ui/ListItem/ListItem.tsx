import React from 'react';
import { View, Text, Pressable, ViewStyle } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { Icon } from '../../icons';
import { Toggle } from '../Toggle';
import { backgrounds, labels, separators } from '../../../theme/colors';
import { spacing, radius } from '../../../theme';
import type { ListItemProps } from './types';
import { getStyles } from './styles';

export const ListItem: React.FC<ListItemProps> = ({
  label,
  icon,
  showToggle = false,
  toggleValue = false,
  onToggleChange,
  onPress,
  position = 'middle',
  showSeparator = true,
  disabled = false,
  style,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles();

  const getContainerStyle = (): ViewStyle => {
    const baseStyle: ViewStyle = {
      backgroundColor: backgrounds.secondary[colorScheme],
    };

    switch (position) {
      case 'first':
        return {
          ...baseStyle,
          borderTopLeftRadius: radius.xl,
          borderTopRightRadius: radius.xl,
        };
      case 'last':
        return {
          ...baseStyle,
          borderBottomLeftRadius: radius.xl,
          borderBottomRightRadius: radius.xl,
        };
      case 'single':
        return {
          ...baseStyle,
          borderRadius: radius.xl,
        };
      default:
        return baseStyle;
    }
  };

  const iconColor = labels.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const separatorColor = separators.primary[colorScheme];

  const content = (
    <View style={[styles.container, getContainerStyle(), style]}>
      <View style={styles.content}>
        {icon && (
          <View style={styles.iconContainer}>
            <Icon name={icon} size={20} color={iconColor} />
          </View>
        )}
        <Text style={[styles.label, { color: textColor }, disabled && styles.labelDisabled]}>
          {label}
        </Text>
        {showToggle && (
          <Toggle
            value={toggleValue}
            onValueChange={onToggleChange ?? (() => {})}
            disabled={disabled}
          />
        )}
      </View>
      {showSeparator && position !== 'last' && position !== 'single' && (
        <View
          style={[
            styles.separator,
            {
              backgroundColor: separatorColor,
              marginLeft: icon ? 48 : spacing.md,
            },
          ]}
        />
      )}
    </View>
  );

  if (showToggle || !onPress) {
    return content;
  }

  return (
    <Pressable onPress={onPress} disabled={disabled}>
      {content}
    </Pressable>
  );
};
