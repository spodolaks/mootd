import { StyleSheet, TextStyle } from 'react-native';
import { button } from '../../../theme';
import { ColorMode } from '../../../theme/colors';
import { typography } from '../../../theme/typography';

/**
 * Button sizes from Figma design:
 *
 * Icon-only buttons (circular):
 * - xs: 28x28, radius 14
 * - sm: 34x34, radius 17
 * - md: 50x50, radius 25
 * - lg: 50x50, radius 25 (same as md for icon-only)
 *
 * Buttons with label (pill shape):
 * - xs: height 28, radius 14
 * - sm: height 32, radius 16
 * - md: height 38, radius 19
 * - lg: height 54, radius 27
 */

// Icon sizes based on button size
const iconSizes = {
  // Icon-only buttons
  iconOnly: {
    xs: 14, // fits in 28px button
    sm: 14, // fits in 34px button
    md: 14, // fits in 50px button
    lg: 24, // larger icon for lg button
  },
  // Buttons with label
  withLabel: {
    xs: 14,
    sm: 14,
    md: 14,
    lg: 24,
  },
};

// Get icon size based on button size and type
export const getIconSize = (size: 'xs' | 'sm' | 'md' | 'lg', isIconOnly?: boolean): number => {
  return isIconOnly ? iconSizes.iconOnly[size] : iconSizes.withLabel[size];
};

// Get text style based on button size
export const getTextStyle = (size: 'xs' | 'sm' | 'md' | 'lg'): TextStyle => {
  switch (size) {
    case 'xs':
      return typography.caption1.semiBold;
    case 'sm':
      return typography.footnote.semiBold;
    case 'md':
      return typography.subheadline.semiBold;
    case 'lg':
      return typography.body.semiBold;
    default:
      return typography.subheadline.semiBold;
  }
};

// Create styles based on color scheme
export const getStyles = (colorScheme: ColorMode) =>
  StyleSheet.create({
    // Base button styles
    button: {
      alignItems: 'center',
      justifyContent: 'center',
      flexDirection: 'row',
    },

    // Content container for icon + label
    contentContainer: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'center',
    },

    // Icon spacing
    iconLeft: {
      marginRight: 6,
    },
    iconRight: {
      marginLeft: 6,
    },

    // Variant styles
    primary: {
      backgroundColor: button.primary.background[colorScheme],
    },
    secondary: {
      backgroundColor: button.secondary.background[colorScheme],
    },
    ghost: {
      backgroundColor: button.ghost.background[colorScheme],
    },

    // Disabled states
    primaryDisabled: {
      opacity: button.primary.disabledOpacity,
    },
    secondaryDisabled: {
      opacity: button.secondary.disabledOpacity,
    },
    ghostDisabled: {
      opacity: button.ghost.disabledOpacity,
    },

    // Text colors
    textPrimary: {
      color: button.primary.foreground[colorScheme],
    },
    textSecondary: {
      color: button.secondary.foreground[colorScheme],
    },
    textGhost: {
      color: button.ghost.foreground[colorScheme],
    },

    // Icon-only sizes (circular)
    iconOnlyXs: {
      width: 28,
      height: 28,
      borderRadius: 14,
    },
    iconOnlySm: {
      width: 34,
      height: 34,
      borderRadius: 17,
    },
    iconOnlyMd: {
      width: 50,
      height: 50,
      borderRadius: 25,
    },
    iconOnlyLg: {
      width: 50,
      height: 50,
      borderRadius: 25,
    },

    // Button with label sizes (pill shape)
    sizeXs: {
      height: 28,
      paddingHorizontal: 12,
      borderRadius: 14,
    },
    sizeSm: {
      height: 32,
      paddingHorizontal: 14,
      borderRadius: 16,
    },
    sizeMd: {
      height: 38,
      paddingHorizontal: 16,
      borderRadius: 19,
    },
    sizeLg: {
      height: 54,
      paddingHorizontal: 20,
      borderRadius: 27,
    },
  });
