import React from 'react';
import { View, Text, Pressable, ViewStyle } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { Icon } from '../../icons';
import { labels } from '../../../theme';
import { getStyles, getIconSize, getTextStyle } from './styles';
import type { ChipProps } from './types';

export const Chip: React.FC<ChipProps> = ({
  label,
  icon,
  size = 'default',
  disabled,
  style,
  ...props
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const isIconOnly = !label && !!icon;
  const styles = getStyles(colorScheme);
  const iconSize = getIconSize(size);
  const textStyle = getTextStyle(size);
  const iconColor = labels.primary[colorScheme];

  const renderIcon = () => {
    if (!icon) return null;
    return <Icon name={icon} size={iconSize} color={iconColor} />;
  };

  const renderContent = () => {
    if (isIconOnly) {
      return renderIcon();
    }

    return (
      <>
        {icon && <View style={styles.iconLeft}>{renderIcon()}</View>}
        {label && <Text style={[textStyle, styles.label]}>{label}</Text>}
      </>
    );
  };

  // Get chip style based on size and icon-only state
  const getChipStyle = (): ViewStyle[] => {
    const baseStyles: ViewStyle[] = [styles.chip];

    if (isIconOnly) {
      const sizeKey =
        `iconOnly${size.charAt(0).toUpperCase()}${size.slice(1)}` as keyof typeof styles;
      baseStyles.push(styles[sizeKey] as ViewStyle);
    } else {
      const sizeKey = `size${size.charAt(0).toUpperCase()}${size.slice(1)}` as keyof typeof styles;
      baseStyles.push(styles[sizeKey] as ViewStyle);
    }

    return baseStyles;
  };

  return (
    <Pressable
      style={[...getChipStyle(), disabled && { opacity: 0.4 }, style]}
      disabled={disabled}
      {...props}>
      {renderContent()}
    </Pressable>
  );
};
