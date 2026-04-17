import { StyleSheet } from 'react-native';
import { ColorMode } from '../../../theme/colors';

export const getStyles = (colorScheme: ColorMode) =>
  StyleSheet.create({
    container: {
      flex: 1,
    },
    touchable: {
      flex: 1,
    },
    imageContainer: {
      aspectRatio: 3 / 4,
      borderRadius: 24,
      overflow: 'hidden',
      position: 'relative',
    },
    image: {
      width: '100%',
      height: '100%',
    },
    placeholder: {
      width: '100%',
      height: '100%',
    },
    checkBadge: {
      position: 'absolute',
      top: 12,
      right: 12,
      width: 32,
      height: 32,
      borderRadius: 16,
      justifyContent: 'center',
      alignItems: 'center',
    },
    label: {
      marginTop: 8,
      textAlign: 'center',
    },
    disabled: {
      opacity: 0.5,
    },
  });
