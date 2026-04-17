import React, { useState } from 'react';
import { View, StyleSheet, ScrollView } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Text, List, Modal } from '@/src/components';
import { backgrounds, labels } from '@/src/theme/colors';
import { useColorScheme } from '@/src/hooks';

export const ReadingListScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const [toggleValue, setToggleValue] = useState(true);
  const [modalVisible, setModalVisible] = useState(false);

  const backgroundColor = backgrounds.primary[colorScheme];
  const descriptionColor = labels.tertiary[colorScheme];

  const handleToggleChange = (value: boolean) => {
    setToggleValue(value);
  };

  const handleItemPress = () => {
    setModalVisible(true);
  };

  const handleModalDismiss = () => {
    setModalVisible(false);
  };

  const handleModalButtonPress = () => {
    // TODO: implement
    setModalVisible(false);
  };

  // First section with toggle on first item
  const section1Items = [
    {
      label: 'Add to Reading List',
      icon: 'plus' as const,
      showToggle: true,
      toggleValue: toggleValue,
      onToggleChange: handleToggleChange,
    },
    {
      label: 'Add to Reading List',
      icon: 'plus' as const,
      onPress: handleItemPress,
    },
    {
      label: 'Add to Reading List',
      icon: 'plus' as const,
      onPress: handleItemPress,
    },
  ];

  // Second and third sections without toggle
  const sectionItems = [
    {
      label: 'Add to Reading List',
      icon: 'plus' as const,
      onPress: handleItemPress,
    },
    {
      label: 'Add to Reading List',
      icon: 'plus' as const,
      onPress: handleItemPress,
    },
    {
      label: 'Add to Reading List',
      icon: 'plus' as const,
      onPress: handleItemPress,
    },
  ];

  return (
    <>
      <SafeAreaView style={[styles.container, { backgroundColor }]}>
        <ScrollView style={styles.scrollView} contentContainerStyle={styles.content}>
          {/* Title */}
          <View style={styles.headerSection}>
            <Text variant="largeTitle" weight="semiBold" style={styles.title}>
              Title
            </Text>
          </View>

          {/* Section 1 */}
          <Text variant="body" color={descriptionColor} style={styles.sectionDescription}>
            Description
          </Text>
          <List items={section1Items} style={styles.listSection} />

          {/* Section 2 */}
          <Text variant="body" color={descriptionColor} style={styles.sectionDescription}>
            Description
          </Text>
          <List items={sectionItems} style={styles.listSection} />

          {/* Section 3 */}
          <Text variant="body" color={descriptionColor} style={styles.sectionDescription}>
            Description
          </Text>
          <List items={sectionItems} style={styles.listSection} />
        </ScrollView>
      </SafeAreaView>

      <Modal
        visible={modalVisible}
        title="Title"
        description="Description"
        buttonLabel="Label"
        onButtonPress={handleModalButtonPress}
        onDismiss={handleModalDismiss}
      />
    </>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  scrollView: {
    flex: 1,
  },
  content: {
    paddingHorizontal: 16,
    paddingBottom: 32,
  },
  headerSection: {
    marginTop: 24,
    marginBottom: 16,
  },
  title: {
    // Large title styling
  },
  sectionDescription: {
    marginBottom: 8,
  },
  listSection: {
    marginBottom: 24,
  },
});
