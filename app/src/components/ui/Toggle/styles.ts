import { StyleSheet } from 'react-native';
import { grays } from '../../../theme/colors';

// Component-specific dimensions (no matching theme tokens)
const TRACK_WIDTH = 64;
const TRACK_HEIGHT = 28;
const THUMB_WIDTH = 39;
const THUMB_HEIGHT = 24;

export const getStyles = () => {
  return StyleSheet.create({
    container: {
      justifyContent: 'center',
    },
    track: {
      width: TRACK_WIDTH,
      height: TRACK_HEIGHT,
      borderRadius: TRACK_HEIGHT / 2,
      justifyContent: 'center',
    },
    thumb: {
      width: THUMB_WIDTH,
      height: THUMB_HEIGHT,
      borderRadius: THUMB_HEIGHT / 2,
      shadowColor: grays.black.light,
      shadowOffset: {
        width: 0,
        height: 2,
      },
      shadowOpacity: 0.2,
      shadowRadius: 2,
      elevation: 2,
    },
  });
};
