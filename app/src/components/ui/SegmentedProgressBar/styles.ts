import { StyleSheet } from 'react-native';

export const styles = StyleSheet.create({
  wrapper: {},
  wrapperWithFade: {
    zIndex: 1,
  },
  container: {
    flexDirection: 'row',
    gap: 12,
  },
  segment: {
    flex: 1,
    height: 4,
    borderRadius: 2,
  },
  fade: {
    height: 24,
    marginBottom: -24,
  },
});
