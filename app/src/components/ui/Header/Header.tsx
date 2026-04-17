import { useColorScheme } from '@/src/hooks';
import React from 'react';
import { View } from 'react-native';
import { getStyles } from './styles';
import type { HeaderProps } from './types';

export const Header: React.FC<HeaderProps> = ({ style, topContent, bottomContent }) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);

  return (
    <View style={[styles.container, style]}>
      <View style={styles.topSection}>{topContent}</View>
      <View style={styles.bottomSection}>{bottomContent}</View>
    </View>
  );
};
