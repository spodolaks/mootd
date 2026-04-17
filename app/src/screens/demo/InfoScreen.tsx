import React from 'react';
import { View, StyleSheet, ScrollView } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Text, Info } from '@/src/components';
import { backgrounds, separators } from '@/src/theme/colors';
import { useColorScheme } from '@/src/hooks';

export const InfoScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';

  const backgroundColor = backgrounds.primary[colorScheme];
  const cardBackgroundColor = backgrounds.secondary[colorScheme];
  const cardBorderColor = separators.secondary[colorScheme];

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={styles.scrollContent}
        showsVerticalScrollIndicator={false}>
        {/* Title */}
        <Text variant="largeTitle" weight="semiBold" style={styles.title}>
          Title
        </Text>

        {/* Info Component */}
        <Info title="Info Title" description="Description" onClose={() => {}} style={styles.info} />

        {/* Cards Section */}
        <View style={styles.cardsContainer}>
          {/* Large Card 1 */}
          <View
            style={[
              styles.largeCard,
              {
                backgroundColor: cardBackgroundColor,
                borderColor: cardBorderColor,
              },
            ]}
          />

          {/* Two Cards Row */}
          <View style={styles.twoCardsRow}>
            <View
              style={[
                styles.halfCard,
                {
                  backgroundColor: cardBackgroundColor,
                  borderColor: cardBorderColor,
                },
              ]}
            />
            <View
              style={[
                styles.halfCard,
                {
                  backgroundColor: cardBackgroundColor,
                  borderColor: cardBorderColor,
                },
              ]}
            />
          </View>

          {/* Large Card 2 */}
          <View
            style={[
              styles.largeCard,
              {
                backgroundColor: cardBackgroundColor,
                borderColor: cardBorderColor,
              },
            ]}
          />
        </View>
      </ScrollView>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {
    paddingHorizontal: 16,
    paddingBottom: 24,
  },
  title: {
    marginTop: 12,
    marginBottom: 16,
  },
  info: {
    marginBottom: 16,
  },
  cardsContainer: {
    gap: 8,
  },
  largeCard: {
    width: '100%',
    height: 164,
    borderRadius: 24,
    borderWidth: 1,
  },
  twoCardsRow: {
    flexDirection: 'row',
    gap: 8,
  },
  halfCard: {
    flex: 1,
    height: 164,
    borderRadius: 24,
    borderWidth: 1,
  },
});
