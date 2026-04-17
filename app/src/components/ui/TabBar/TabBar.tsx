import React from 'react';
import { View } from 'react-native';
import { AddTab } from '../AddTab';
import { getStyles } from './styles';
import { useColorScheme } from '@/src/hooks';
import type { TabBarProps } from './types';

export const TabBar: React.FC<TabBarProps> = ({
  tabs,
  selectedId,
  onTabPress,
  style,
  ...props
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);

  return (
    <View style={[styles.container, style]} {...props}>
      <View style={styles.tabsRow}>
        {tabs.map(tab => (
          <AddTab
            key={tab.id}
            label={tab.label}
            icon={tab.icon}
            disabled={tab.disabled}
            selected={tab.id === selectedId}
            onPress={() => onTabPress?.(tab)}
            style={styles.tab}
          />
        ))}
      </View>
    </View>
  );
};
