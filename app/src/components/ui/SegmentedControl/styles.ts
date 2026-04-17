import { StyleSheet, TextStyle, ViewStyle } from 'react-native';
import { ColorMode } from '../../../theme/colors';
import { backgrounds, fills, labels } from '../../../theme';
import { typography } from '../../../theme/typography';

const CONTAINER_HEIGHT = 36;
const CONTAINER_PADDING = 4;
const INDICATOR_HEIGHT = 28;
const BORDER_RADIUS = CONTAINER_HEIGHT / 2;
const INDICATOR_RADIUS = INDICATOR_HEIGHT / 2;

interface SegmentedControlStyles {
  container: ViewStyle;
  indicator: ViewStyle;
  segment: ViewStyle;
  segmentText: TextStyle;
  segmentTextSelected: TextStyle;
  disabled: ViewStyle;
}

export const getStyles = (colorScheme: ColorMode): SegmentedControlStyles => {
  return StyleSheet.create<SegmentedControlStyles>({
    container: {
      flexDirection: 'row',
      alignItems: 'center',
      height: CONTAINER_HEIGHT,
      backgroundColor: backgrounds.secondary[colorScheme],
      borderRadius: BORDER_RADIUS,
      padding: CONTAINER_PADDING,
    },
    indicator: {
      position: 'absolute',
      height: INDICATOR_HEIGHT,
      backgroundColor: fills.tertiary[colorScheme],
      borderRadius: INDICATOR_RADIUS,
      // Shadow matching Figma design
      shadowColor: '#000',
      shadowOffset: {
        width: 0,
        height: 2,
      },
      shadowOpacity: 0.06,
      shadowRadius: 10,
      elevation: 2,
    },
    segment: {
      flex: 1,
      height: INDICATOR_HEIGHT,
      justifyContent: 'center',
      alignItems: 'center',
      paddingHorizontal: 12,
    },
    segmentText: {
      ...typography.subheadline.regular,
      color: labels.primary[colorScheme],
    },
    segmentTextSelected: {
      ...typography.subheadline.semiBold,
      color: labels.primary[colorScheme],
    },
    disabled: {
      opacity: 0.5,
    },
  });
};
