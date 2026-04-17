import { StyleSheet } from 'react-native';
import type { ColorMode } from '../../../theme/colors';
import { button } from '../../../theme/colors';
import { typography } from '../../../theme/typography';

export const BUTTON_HEIGHT = 54;
export const BORDER_RADIUS = 27;
export const GLOW_HEIGHT = 50;

export const getStyles = (colorScheme: ColorMode) => {
  const textColor = button.primary.foreground[colorScheme];

  return StyleSheet.create({
    wrapper: {
      position: 'relative',
      paddingBottom: 20,
      alignItems: 'center',
    },
    glowContainer: {
      position: 'absolute',
      bottom: 0,
      left: -16,
      right: -16,
      height: GLOW_HEIGHT,
    },
    touchable: {
      position: 'relative',
      height: BUTTON_HEIGHT,
      // Explicit width ensures the touch target fills the wrapper even before
      // useWindowDimensions returns the real value on web.
      width: '100%',
    },
    content: {
      position: 'absolute',
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'center',
    },
    iconLeft: {
      marginRight: 6,
    },
    iconRight: {
      marginLeft: 6,
    },
    label: {
      ...typography.body.semiBold,
      color: textColor,
    },
    disabled: {
      opacity: 0.5,
    },
  });
};
