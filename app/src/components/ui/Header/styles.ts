import { StyleSheet } from 'react-native';
import { radius } from '../../../theme';
import { ColorMode, grays, backgrounds } from '../../../theme/colors';

export const getStyles = (colorScheme: ColorMode) =>
  StyleSheet.create({
    container: {
      width: '100%',
      overflow: 'hidden',
      borderRadius: radius.xl,
    },
    topSection: {
      height: 160,
      backgroundColor: backgrounds.primary[colorScheme],
      borderTopLeftRadius: radius.xl,
      borderTopRightRadius: radius.xl,
    },
    bottomSection: {
      height: 128,
      backgroundColor: grays.black[colorScheme],
      borderBottomLeftRadius: radius.xl,
      borderBottomRightRadius: radius.xl,
    },
  });
