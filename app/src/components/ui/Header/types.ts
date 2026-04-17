import type { StyleProp, ViewStyle } from 'react-native';

export interface HeaderProps {
  style?: StyleProp<ViewStyle>;
  topContent?: React.ReactNode;
  bottomContent?: React.ReactNode;
}
