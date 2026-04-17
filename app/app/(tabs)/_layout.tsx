import { Tabs } from 'expo-router';
import React from 'react';

import { HapticTab, Icon } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { colors } from '@/src/theme';

export default function TabLayout() {
  const colorScheme = useColorScheme();

  return (
    <Tabs
      initialRouteName="explore"
      screenOptions={{
        tabBarActiveTintColor: colors.accents.brand[colorScheme],
        headerShown: false,
        tabBarButton: HapticTab,
        tabBarStyle: { display: 'none' },
      }}>
      <Tabs.Screen
        name="explore"
        options={{
          title: 'Explore',
          tabBarIcon: ({ color }) => <Icon name="compass" size={28} color={color} />,
        }}
      />
      <Tabs.Screen
        name="selectable"
        options={{
          title: 'Selectable',
          tabBarIcon: ({ color }) => <Icon name="check" size={28} color={color} />,
        }}
      />
      <Tabs.Screen
        name="gradient"
        options={{
          title: 'Gradient',
          tabBarIcon: ({ color }) => <Icon name="idea" size={28} color={color} />,
        }}
      />
      <Tabs.Screen
        name="onboarding"
        options={{
          title: 'Reading List',
          tabBarIcon: ({ color }) => <Icon name="file" size={28} color={color} />,
        }}
      />
      <Tabs.Screen
        name="info"
        options={{
          title: 'Info',
          tabBarIcon: ({ color }) => <Icon name="info" size={28} color={color} />,
        }}
      />
      <Tabs.Screen
        name="tiles"
        options={{
          title: 'Tiles',
          tabBarIcon: ({ color }) => <Icon name="menu" size={28} color={color} />,
        }}
      />
    </Tabs>
  );
}
