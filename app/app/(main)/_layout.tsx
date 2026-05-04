import { Tabs } from 'expo-router';
import React from 'react';
import { View, Pressable, StyleSheet, Platform } from 'react-native';
import { BottomTabBarProps } from '@react-navigation/bottom-tabs';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AddTab } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, separators } from '@/src/theme/colors';
import type { IconName } from '@/src/components';

const TAB_CONFIG: Record<string, { icon: IconName; label: string }> = {
  moodboard: { icon: 'sunrise', label: 'Moodboard' },
  wardrobe: { icon: 'closet', label: 'Wardrobe' },
  calendar: { icon: 'calendar', label: 'Calendar' },
  profile: { icon: 'user', label: 'Profile' },
};

// Floating pill geometry — kept together so screens can import PILL_GUTTER
// if they need to reserve bottom padding for content that would otherwise
// be occluded by the pill.
const PILL_HORIZONTAL_MARGIN = 16;
const PILL_BOTTOM_MARGIN = 12;
const PILL_HEIGHT = 64;
const PILL_RADIUS = 32;

/** Total clearance the pill needs from the bottom edge (pill + bottom gap). */
export const PILL_GUTTER = PILL_HEIGHT + PILL_BOTTOM_MARGIN;

/**
 * Returns the bottom padding every tab screen should apply so its content
 * clears the floating pill on any device. Accounts for the home-indicator
 * inset on modern iPhones and falls back to a 12px baseline elsewhere.
 */
export const useTabContentBottomPadding = (): number => {
  const insets = useSafeAreaInsets();
  return Math.max(insets.bottom, PILL_BOTTOM_MARGIN) + PILL_HEIGHT + 8;
};

function FloatingPillTabBar({ state, descriptors: _descriptors, navigation }: BottomTabBarProps) {
  const colorScheme = useColorScheme() ?? 'light';
  const insets = useSafeAreaInsets();

  // The pill sits above the home-indicator / gesture area: use either the
  // device inset or a baseline margin, whichever is larger, so it never
  // crowds the OS UI on notched + bezel-less phones.
  const bottomOffset = Math.max(insets.bottom, PILL_BOTTOM_MARGIN);

  const pillBackground = backgrounds.secondary[colorScheme];
  const borderColor = separators.primary[colorScheme];

  return (
    <View
      style={[
        styles.pill,
        {
          bottom: bottomOffset,
          backgroundColor: pillBackground,
          borderColor,
        },
      ]}
      // The absolute pill spans the full width via side margins, but only the
      // pill itself should receive taps — `box-none` lets taps pass through
      // the surrounding gap to the content underneath.
      pointerEvents="box-none"
    >
      <View style={styles.pillInner} pointerEvents="auto">
        {state.routes.map((route, index) => {
          const isFocused = state.index === index;
          const config = TAB_CONFIG[route.name] || { icon: 'home' as IconName, label: route.name };

          const onPress = () => {
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
              hitSlop={8}
              accessibilityRole="button"
              accessibilityState={isFocused ? { selected: true } : {}}
              accessibilityLabel={config.label}
              testID={`tab-${route.name}`}
            >
              <AddTab icon={config.icon} label={config.label} selected={isFocused} />
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
      tabBar={(props) => <FloatingPillTabBar {...props} />}
      screenOptions={{
        headerShown: false,
        // `position: absolute` tells react-navigation not to reserve height
        // for the tab bar, so screens render full-bleed behind the pill.
        // Screens with a fixed-bottom CTA should add PILL_GUTTER to their
        // own bottom padding so the button isn't hidden.
        tabBarStyle: { position: 'absolute' },
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
  pill: {
    position: 'absolute',
    left: PILL_HORIZONTAL_MARGIN,
    right: PILL_HORIZONTAL_MARGIN,
    height: PILL_HEIGHT,
    borderRadius: PILL_RADIUS,
    borderWidth: StyleSheet.hairlineWidth,
    // Shadow recipe matches the moodboard panel/card depth elsewhere in the
    // app so the pill reads as part of the same visual system.
    ...Platform.select({
      ios: {
        shadowColor: '#000',
        shadowOpacity: 0.25,
        shadowOffset: { width: 0, height: 8 },
        shadowRadius: 20,
      },
      android: {
        elevation: 10,
      },
      web: {
        // RN Web maps shadow* to box-shadow; set it explicitly so the ambient
        // drop reads on web without needing platform-native shadow support.
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        boxShadow: '0 8px 20px rgba(0,0,0,0.25)' as any,
      },
      default: {},
    }),
  },
  pillInner: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-around',
    paddingHorizontal: 8,
  },
  tab: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: 8,
  },
});
