import { StyleSheet } from 'react-native';
import { typography } from '../../../theme/typography';
import { spacing } from '../../../theme/spacing';

export const getStyles = () => {
  return StyleSheet.create({
    container: {
      width: '100%',
    },
    header: {
      fontFamily: typography.footnote.regular.fontFamily,
      fontSize: typography.footnote.regular.fontSize,
      lineHeight: typography.footnote.regular.lineHeight,
      textTransform: 'uppercase',
      marginBottom: spacing.xs,
      marginLeft: spacing.md,
    },
    itemsContainer: {
      overflow: 'hidden',
    },
    footer: {
      fontFamily: typography.footnote.regular.fontFamily,
      fontSize: typography.footnote.regular.fontSize,
      lineHeight: typography.footnote.regular.lineHeight,
      marginTop: spacing.xs,
      marginLeft: spacing.md,
    },
  });
};
