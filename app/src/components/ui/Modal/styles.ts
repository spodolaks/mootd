import { StyleSheet } from 'react-native';
import { ColorMode, fills } from '../../../theme/colors';
import { backgrounds, labels, button, overlays } from '../../../theme';
import { typography } from '../../../theme/typography';
import { spacing } from '../../../theme/spacing';

export const getStyles = (colorScheme: ColorMode) =>
  StyleSheet.create({
    // Full screen wrapper
    modalWrapper: {
      flex: 1,
      justifyContent: 'flex-end',
    },

    // Overlay backdrop (absolute positioned for animation)
    overlay: {
      ...StyleSheet.absoluteFillObject,
      backgroundColor: overlays.default[colorScheme],
    },

    // Pressable area for overlay
    overlayPressable: {
      flex: 1,
    },

    // Main modal container - single white background
    container: {
      backgroundColor: backgrounds.secondary[colorScheme],
      borderTopLeftRadius: 24,
      borderTopRightRadius: 24,
    },

    // Header area with grabber
    header: {
      alignItems: 'center',
      paddingTop: 8,
    },

    // Grabber handle
    grabber: {
      width: 36,
      height: 5,
      borderRadius: 2.5,
      backgroundColor: fills.secondary[colorScheme],
    },

    // Content area
    content: {
      paddingHorizontal: 16,
      paddingTop: 22,
    },

    // Title text (bold heading)
    title: {
      ...typography.title3.semiBold,
      color: labels.primary[colorScheme],
      marginBottom: 16,
    },

    // Description text (body size)
    description: {
      ...typography.body.regular,
      color: labels.primary[colorScheme],
      marginBottom: 24,
    },

    // Button styles
    button: {
      height: 54,
      borderRadius: 27,
      backgroundColor: button.primary.background[colorScheme],
      alignItems: 'center',
      justifyContent: 'center',
    },

    buttonText: {
      ...typography.body.semiBold,
      color: button.primary.foreground[colorScheme],
    },

    // Footer padding for safe area
    footer: {
      paddingBottom: spacing.lg,
    },
  });
