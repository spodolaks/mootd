import { StyleSheet, TextStyle } from 'react-native';
import { fills, labels, spacing } from '../../../theme';
import { ColorMode } from '../../../theme/colors';
import { typography } from '../../../theme/typography';

/**
 * Chip sizes from Figma design:
 *
 * - default: height 28, borderRadius 14 (pill shape)
 * - small: height 22, borderRadius 11 (pill shape)
 */

// Icon sizes based on chip size
const iconSizes = {
  default: 14,
  small: 12,
};

// Get icon size based on chip size
export const getIconSize = (size: 'default' | 'small'): number => {
  return iconSizes[size];
};

// Get text style based on chip size
export const getTextStyle = (size: 'default' | 'small'): TextStyle => {
  return size === 'default' ? typography.caption1.regular : typography.caption2.regular;
};

// Create styles based on color scheme
export const getStyles = (colorScheme: ColorMode) =>
  StyleSheet.create({
    // Base chip container
    chip: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: fills.secondary[colorScheme],
    },

    // Size variants
    sizeDefault: {
      height: 28,
      paddingHorizontal: 12,
      borderRadius: 14,
    },
    sizeSmall: {
      height: 22,
      paddingHorizontal: 10,
      borderRadius: 11,
    },

    // Icon-only variants (circular)
    iconOnlyDefault: {
      width: 28,
      height: 28,
      borderRadius: 14,
      paddingHorizontal: 0,
    },
    iconOnlySmall: {
      width: 22,
      height: 22,
      borderRadius: 11,
      paddingHorizontal: 0,
    },

    // Icon spacing when label is present
    iconLeft: {
      marginRight: spacing.xs,
    },

    // Text color
    label: {
      color: labels.primary[colorScheme],
    },
  });
