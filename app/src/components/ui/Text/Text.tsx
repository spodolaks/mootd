import React from 'react';
import { Text as RNText } from 'react-native';
import { typography } from '../../../theme';
import { useColorScheme } from '@/src/hooks';
import type { TextProps } from './types';
import { getStyles } from './styles';

export const Text: React.FC<TextProps> = ({
  variant = 'body',
  weight = 'regular',
  color,
  style,
  children,
  ...props
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);
  const textColor = color ?? styles.base.color;

  // Get the typography style, handling variants that might not have both weights
  const variantStyles = typography[variant];
  const typographyStyle =
    weight in variantStyles
      ? variantStyles[weight as keyof typeof variantStyles]
      : 'regular' in variantStyles
        ? variantStyles.regular
        : variantStyles.semiBold;

  return (
    <RNText style={[typographyStyle, { color: textColor }, style]} {...props}>
      {children}
    </RNText>
  );
};
