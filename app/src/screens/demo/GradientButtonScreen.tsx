import React, { useState } from 'react';
import { View, StyleSheet } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Text, SelectableItem, ProgressBar, GradientButton } from '@/src/components';
import { backgrounds } from '@/src/theme/colors';
import { useColorScheme } from '@/src/hooks';

export const GradientButtonScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const [selectedIndex, setSelectedIndex] = useState<number>(1);

  const backgroundColor = backgrounds.primary[colorScheme];

  const handleItemPress = (index: number) => {
    setSelectedIndex(index);
  };

  const handleButtonPress = () => {
    // TODO: implement
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]}>
      <View style={styles.content}>
        {/* Progress Bar */}
        <ProgressBar progress={0.5} style={styles.progressBar} />

        {/* Title and Description */}
        <View style={styles.headerSection}>
          <Text variant="largeTitle" weight="semiBold" style={styles.title}>
            Title
          </Text>
          <Text variant="body" style={styles.description}>
            Description
          </Text>
        </View>

        {/* Selectable Items */}
        <View style={styles.itemsContainer}>
          <SelectableItem
            label="Simple Selectable Element"
            selected={selectedIndex === 0}
            onPress={() => handleItemPress(0)}
            style={styles.selectableItem}
          />
          <SelectableItem
            label="Simple Selectable Element"
            selected={selectedIndex === 1}
            onPress={() => handleItemPress(1)}
            style={styles.selectableItem}
          />
          <SelectableItem
            label="Simple Selectable Element"
            variant="withIcon"
            selected={selectedIndex === 2}
            onPress={() => handleItemPress(2)}
            style={styles.selectableItem}
          />
        </View>

        {/* Spacer */}
        <View style={styles.spacer} />

        {/* Gradient Button */}
        <GradientButton
          label="Label"
          icon="plus"
          onPress={handleButtonPress}
          style={styles.button}
        />
      </View>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  content: {
    flex: 1,
    paddingHorizontal: 16,
  },
  progressBar: {
    marginTop: 16,
  },
  headerSection: {
    marginTop: 24,
    marginBottom: 24,
  },
  title: {
    marginBottom: 8,
  },
  description: {
    // Uses primary label color (black) with body variant
  },
  itemsContainer: {
    gap: 8,
  },
  selectableItem: {
    // Individual item styles if needed
  },
  spacer: {
    flex: 1,
  },
  button: {
    marginBottom: 16,
  },
});
