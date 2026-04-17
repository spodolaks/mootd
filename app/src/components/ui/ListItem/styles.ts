import { StyleSheet } from 'react-native';
import { typography } from '../../../theme/typography';
import { spacing } from '../../../theme/spacing';

const ITEM_HEIGHT = 52;

export const getStyles = () => {
  return StyleSheet.create({
    container: {
      minHeight: ITEM_HEIGHT,
    },
    content: {
      flexDirection: 'row',
      alignItems: 'center',
      paddingHorizontal: spacing.md,
      paddingVertical: spacing.sm + 4,
      minHeight: ITEM_HEIGHT,
    },
    iconContainer: {
      width: 24,
      height: 24,
      justifyContent: 'center',
      alignItems: 'center',
      marginRight: spacing.sm,
    },
    label: {
      flex: 1,
      fontFamily: typography.body.regular.fontFamily,
      fontSize: typography.body.regular.fontSize,
      lineHeight: typography.body.regular.lineHeight,
    },
    labelDisabled: {
      opacity: 0.5,
    },
    separator: {
      height: 1,
      position: 'absolute',
      bottom: 0,
      right: 0,
      left: 0,
    },
  });
};
