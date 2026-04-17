import { StyleSheet } from 'react-native';

export const getStyles = () => {
  return StyleSheet.create({
    container: {
      width: '100%',
      overflow: 'hidden',
    },
    progress: {
      position: 'absolute',
      left: 0,
      top: 0,
    },
  });
};
