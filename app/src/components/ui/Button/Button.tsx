import React from 'react';
import { Pressable, Text, View, ViewStyle } from 'react-native';
import { Icon } from '../../icons';
import { getStyles, getIconSize, getTextStyle } from './styles';
import { useColorScheme } from '@/src/hooks';
import { button } from '../../../theme';
import type { ButtonProps } from './types';

export const Button: React.FC<ButtonProps> = ({
  label,
  icon,
  iconPosition = 'left',
  variant = 'primary',
  size = 'md',
  disabled,
  style,
  ...props
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const isIconOnly = !label && !!icon;
  const styles = getStyles(colorScheme);
  const iconSize = getIconSize(size, isIconOnly);
  const textStyle = getTextStyle(size);

  // Determine icon color based on variant and color scheme
  const getIconColor = () => {
    if (variant === 'primary') {
      return button.primary.foreground[colorScheme];
    }
    return button.secondary.foreground[colorScheme];
  };

  const renderIcon = () => {
    if (!icon) return null;
    return <Icon name={icon} size={iconSize} color={getIconColor()} />;
  };

  const renderContent = () => {
    if (isIconOnly) {
      return renderIcon();
    }

    return (
      <View style={styles.contentContainer}>
        {icon && iconPosition === 'left' && <View style={styles.iconLeft}>{renderIcon()}</View>}
        {label && (
          <Text
            style={[textStyle, variant === 'primary' ? styles.textPrimary : styles.textSecondary]}>
            {label}
          </Text>
        )}
        {icon && iconPosition === 'right' && <View style={styles.iconRight}>{renderIcon()}</View>}
      </View>
    );
  };

  // Get button style based on variant, size, and icon-only state
  const getButtonStyle = (): ViewStyle[] => {
    const baseStyles: ViewStyle[] = [styles.button];

    // Add variant styles
    baseStyles.push(styles[variant]);

    // Add size styles
    if (isIconOnly) {
      const sizeKey =
        `iconOnly${size.charAt(0).toUpperCase()}${size.slice(1)}` as keyof typeof styles;
      baseStyles.push(styles[sizeKey] as ViewStyle);
    } else {
      const sizeKey = `size${size.charAt(0).toUpperCase()}${size.slice(1)}` as keyof typeof styles;
      baseStyles.push(styles[sizeKey] as ViewStyle);
    }

    // Add disabled styles
    if (disabled) {
      const disabledKey = `${variant}Disabled` as keyof typeof styles;
      baseStyles.push(styles[disabledKey] as ViewStyle);
    }

    return baseStyles;
  };

  return (
    <Pressable
      style={[...getButtonStyle(), style]}
      disabled={disabled}
      {...props}>
      {renderContent()}
    </Pressable>
  );
};
