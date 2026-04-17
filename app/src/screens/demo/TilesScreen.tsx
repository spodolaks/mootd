import React, { useState, useRef } from 'react';
import {
  View,
  StyleSheet,
  ScrollView,
  NativeScrollEvent,
  NativeSyntheticEvent,
  useWindowDimensions,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Text, Toast } from '@/src/components';
import { SlideIndicator } from '@/src/components/ui/SlideIndicator';
import { backgrounds, separators, accents } from '@/src/theme/colors';
import { useColorScheme } from '@/src/hooks';

const HORIZONTAL_PADDING = 16;
const TOTAL_SLIDES = 3;

export const TilesScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const [activeSlide, setActiveSlide] = useState(0);
  const { width: screenWidth } = useWindowDimensions();
  const carouselRef = useRef<ScrollView>(null);

  const cardWidth = screenWidth - HORIZONTAL_PADDING * 2;

  const backgroundColor = backgrounds.primary[colorScheme];
  const cardBackgroundColor = backgrounds.secondary[colorScheme];
  const cardBorderColor = separators.secondary[colorScheme];

  const handleScroll = (event: NativeSyntheticEvent<NativeScrollEvent>) => {
    const offsetX = event.nativeEvent.contentOffset.x;
    const currentIndex = Math.round(offsetX / cardWidth);
    if (currentIndex !== activeSlide && currentIndex >= 0 && currentIndex < TOTAL_SLIDES) {
      setActiveSlide(currentIndex);
    }
  };

  const slideColors = [
    accents.blue[colorScheme],
    accents.purple[colorScheme],
    accents.orange[colorScheme],
  ];

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

        {/* Cards Section */}
        <View style={styles.cardsContainer}>
          {/* Carousel Card with Slide Indicator */}
          <View
            style={[
              styles.carouselContainer,
              {
                backgroundColor: cardBackgroundColor,
                borderColor: cardBorderColor,
              },
            ]}>
            <ScrollView
              ref={carouselRef}
              horizontal
              pagingEnabled
              showsHorizontalScrollIndicator={false}
              onScroll={handleScroll}
              scrollEventThrottle={16}
              decelerationRate="fast"
              snapToInterval={cardWidth}
              snapToAlignment="start"
              contentContainerStyle={styles.carouselContent}>
              {Array.from({ length: TOTAL_SLIDES }).map((_, index) => (
                <View key={index} style={[styles.slide, { width: cardWidth }]}>
                  <View
                    style={[styles.slideContent, { backgroundColor: slideColors[index] + '20' }]}>
                    <Text variant="title3" weight="semiBold" style={{ color: slideColors[index] }}>
                      Slide {index + 1}
                    </Text>
                  </View>
                </View>
              ))}
            </ScrollView>
            <SlideIndicator
              totalDots={TOTAL_SLIDES}
              activeIndex={activeSlide}
              style={styles.slideIndicator}
            />
          </View>

          {/* Two Cards Row 1 */}
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

          {/* Two Cards Row 2 */}
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
        </View>
      </ScrollView>

      {/* Toast at bottom */}
      <Toast title="Toast Title" icon="plus" style={styles.toast} />
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
  cardsContainer: {
    gap: 8,
  },
  carouselContainer: {
    width: '100%',
    height: 164,
    borderRadius: 24,
    borderWidth: 1,
    overflow: 'hidden',
  },
  carouselContent: {
    alignItems: 'stretch',
  },
  slide: {
    height: 164 - 40,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 16,
  },
  slideContent: {
    flex: 1,
    width: '100%',
    borderRadius: 16,
    justifyContent: 'center',
    alignItems: 'center',
    marginTop: 16,
  },
  largeCard: {
    width: '100%',
    height: 164,
    borderRadius: 24,
    borderWidth: 1,
    justifyContent: 'flex-end',
    alignItems: 'center',
    paddingBottom: 20,
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
  slideIndicator: {
    position: 'absolute',
    bottom: 16,
    left: 0,
    right: 0,
  },
  toast: {
    position: 'absolute',
    bottom: 20,
    left: 16,
    right: 16,
  },
});
