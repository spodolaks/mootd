import React from 'react';
import { Pressable, Text, View } from 'react-native';
import Svg, { Path } from 'react-native-svg';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, labels, separators } from '../../../theme/colors';
import { spacing } from '../../../theme';
import type { SelectableItemProps } from './types';
import { getStyles } from './styles';

const PlusIcon: React.FC<{ color: string }> = ({ color }) => (
  <Svg width={16} height={16} viewBox="0 0 16 16" fill="none">
    <Path d="M8 2V14" stroke={color} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
    <Path d="M2 8H14" stroke={color} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
  </Svg>
);

export const SelectableItem: React.FC<SelectableItemProps> = ({
  label,
  selected = false,
  variant = 'simple',
  onPress,
  style,
  disabled,
  ...props
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles();

  const backgroundColor = backgrounds.secondary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const borderColor = selected ? labels.primary[colorScheme] : separators.primary[colorScheme];
  const borderWidth = selected ? 2 : 1;

  return (
    <Pressable
      style={[
        styles.container,
        {
          backgroundColor,
          borderColor,
          borderWidth,
          // Adjust padding to account for border width change
          paddingHorizontal: selected ? spacing.lg - 5 : spacing.lg - 4,
          paddingVertical: selected ? spacing.md - 1 : spacing.md,
        },
        disabled && styles.disabled,
        style,
      ]}
      onPress={onPress}
      disabled={disabled}
      {...props}>
      {variant === 'withIcon' && (
        <View style={styles.iconContainer}>
          <PlusIcon color={textColor} />
        </View>
      )}
      <Text style={[styles.label, { color: textColor }]} numberOfLines={1}>
        {label}
      </Text>
    </Pressable>
  );
};
