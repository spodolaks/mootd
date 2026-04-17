import { Tabs } from 'expo-router';
import React from 'react';
import { View, Pressable, StyleSheet } from 'react-native';
import { BottomTabBarProps } from '@react-navigation/bottom-tabs';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AddTab } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, separators } from '@/src/theme/colors';
import { spacing } from '@/src/theme/spacing';
import type { IconName } from '@/src/components';

const TAB_CONFIG: Record<string, { icon: IconName; label: string }> = {
  moodboard: { icon: 'sunrise', label: 'Moodboard' },
  wardrobe: { icon: 'closet', label: 'Wardrobe' },
  calendar: { icon: 'calendar', label: 'Calendar' },
  profile: { icon: 'user', label: 'Profile' },
};

function CustomTabBar({ state, descriptors, navigation }: BottomTabBarProps) {
  const colorScheme = useColorScheme() ?? 'light';
  const insets = useSafeAreaInsets();

  const backgroundColor = backgrounds.secondary[colorScheme];
  const borderColor = separators.primary[colorScheme];

  return (
    <View
      style={[
        styles.tabBarContainer,
        {
          backgroundColor,
          borderTopColor: borderColor,
          paddingBottom: insets.bottom > 0 ? insets.bottom : spacing.lg,
        },
      ]}
    >
      <View style={styles.tabsRow}>
        {state.routes.map((route, index) => {
          const isFocused = state.index === index;
          const config = TAB_CONFIG[route.name] || { icon: 'home' as IconName, label: route.name };

          const onPress = () => {
            console.log('[TabBar] pressed:', route.name, 'isFocused:', isFocused);
            const event = navigation.emit({
              type: 'tabPress',
              target: route.key,
              canPreventDefault: true,
            });

            if (!isFocused && !event.defaultPrevented) {
              navigation.navigate(route.name, route.params);
            }
          };

          return (
            <Pressable
              key={route.key}
              onPress={onPress}
              style={styles.tab}
            >
              <AddTab
                icon={config.icon}
                label={config.label}
                selected={isFocused}
              />
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

export default function MainTabLayout() {
  return (
    <Tabs
      initialRouteName="moodboard"
      tabBar={(props) => <CustomTabBar {...props} />}
      screenOptions={{
        headerShown: false,
      }}
    >
      <Tabs.Screen name="moodboard" />
      <Tabs.Screen name="wardrobe" />
      <Tabs.Screen name="calendar" />
      <Tabs.Screen name="profile" />
    </Tabs>
  );
}

const styles = StyleSheet.create({
  tabBarContainer: {
    borderTopWidth: 1,
    paddingHorizontal: spacing.md,
    paddingTop: spacing.sm,
  },
  tabsRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'flex-start',
    gap: spacing.sm,
  },
  tab: {
    flex: 1,
  },
});
