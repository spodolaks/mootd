import { StyleSheet } from 'react-native';
import { ColorMode } from '../../../theme/colors';
import { spacing } from '../../../theme/spacing';

const STEP_INDICATOR_WIDTH = 78;
const STEP_INDICATOR_HEIGHT = 54;
const STEP_INDICATOR_RADIUS = STEP_INDICATOR_HEIGHT / 2;

export const getStyles = (colorMode: ColorMode) =>
  StyleSheet.create({
    container: {
      flexDirection: 'row',
      alignItems: 'flex-start',
      justifyContent: 'flex-start',
    },
    stepContainer: {
      alignItems: 'center',
      marginRight: spacing.xs,
    },
    stepIndicator: {
      width: STEP_INDICATOR_WIDTH,
      height: STEP_INDICATOR_HEIGHT,
      borderRadius: STEP_INDICATOR_RADIUS,
      alignItems: 'center',
      justifyContent: 'center',
    },
    label: {
      marginTop: spacing.xs,
      textAlign: 'center',
    },
  });
