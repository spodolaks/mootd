import {
  GradientButton,
  Icon,
  Modal,
} from '@/src/components';
import { useColorScheme, useWeather } from '@/src/hooks';
import { backgrounds, fills, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { radius } from '@/src/theme/radius';
import { wardrobeRepository, moodBoardRepository } from '@/src/data/repositories';
import type { Outfit, SavedMoodBoard, WardrobeItem } from '@/src/domain';
import React, { useState, useCallback, useRef, useMemo } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { Image } from 'expo-image';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useFocusEffect } from '@react-navigation/native';

import { classifyZone } from '@/src/components/moodboard/Collage';
import { OutfitCard } from '@/src/components/moodboard/OutfitCard';
import { SavedBoardView } from '@/src/components/moodboard/SavedBoardView';
import { SCREEN_WIDTH, CONTAINER_PADDING } from '@/src/components/moodboard/constants';
import { useTabContentBottomPadding } from '@/app/(main)/_layout';

type ScreenState = 'loading' | 'empty' | 'generating' | 'choosing' | 'saved';

const buildItemMap = (items: WardrobeItem[]): Map<string, WardrobeItem> => {
  const map = new Map<string, WardrobeItem>();
  items.forEach(item => map.set(item.id, item));
  return map;
};

export const MoodBoardScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const [screenState, setScreenState] = useState<ScreenState>('loading');
  const [outfitOptions, setOutfitOptions] = useState<Outfit[]>([]);
  const [todayBoard, setTodayBoard] = useState<SavedMoodBoard | null>(null);
  const [itemMap, setItemMap] = useState<Map<string, WardrobeItem>>(new Map());
  const [isSaving, setIsSaving] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const [cardHeight, setCardHeight] = useState(0);

  // Swap item modal state
  const [swapTarget, setSwapTarget] = useState<{ outfitIndex: number; itemId: string } | null>(null);

  const { weather } = useWeather();
  const tabBottomPadding = useTabContentBottomPadding();

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];

  const today = new Date().toISOString().split('T')[0];

  const viewabilityConfig = useRef({ itemVisiblePercentThreshold: 50 }).current;
  const onViewableItemsChanged = useRef(({ viewableItems }: { viewableItems: Array<{ index: number | null }> }) => {
    const idx = viewableItems[0]?.index;
    if (idx != null) setActiveIndex(idx);
  }).current;

  const loadData = useCallback(async () => {
    try {
      const [boards, { items }] = await Promise.all([
        moodBoardRepository.list(),
        wardrobeRepository.getItems(),
      ]);
      setItemMap(buildItemMap(items));
      const saved = boards.find(b => b.date === today) ?? null;
      setTodayBoard(saved);
      setScreenState(saved ? 'saved' : 'empty');
    } catch {
      setScreenState('empty');
    }
  }, [today]);

  useFocusEffect(
    useCallback(() => {
      void loadData();
    }, [loadData]),
  );

  const handleGeneratePress = async () => {
    setScreenState('generating');
    try {
      const weatherParams = weather
        ? { temperature: weather.temperature, condition: weather.condition, unit: weather.unit }
        : undefined;

      let outfits: Outfit[];
      try {
        // Submit async job
        const jobId = await wardrobeRepository.submitOutfitGeneration(weatherParams);

        // Poll every 2 seconds until complete
        let result: { status: string; outfits?: Outfit[]; error?: string };
        do {
          await new Promise(resolve => setTimeout(resolve, 2000));
          result = await wardrobeRepository.pollOutfitJob(jobId);
        } while (result.status === 'pending' || result.status === 'processing');

        if (result.status === 'failed') {
          throw new Error(result.error || 'Outfit generation failed');
        }

        outfits = result.outfits ?? [];
      } catch (submitError) {
        // Async not available -- fall back to sync
        console.log('[MoodBoard] Async generation unavailable, falling back to sync');
        outfits = await wardrobeRepository.getOutfits(weatherParams);
      }

      // Also refresh wardrobe items for the collage
      const { items } = await wardrobeRepository.getItems();

      if (outfits.length === 0) {
        Alert.alert('No outfits generated', 'Try adding more items to your wardrobe.');
        setScreenState(todayBoard ? 'saved' : 'empty');
        return;
      }
      setItemMap(buildItemMap(items));
      setOutfitOptions(outfits);
      setActiveIndex(0);
      setScreenState('choosing');
    } catch (e) {
      Alert.alert(
        'Generation Failed',
        e instanceof Error ? e.message : 'Failed to generate outfits.',
        [
          { text: 'Cancel', style: 'cancel', onPress: () => setScreenState(todayBoard ? 'saved' : 'empty') },
          { text: 'Retry', onPress: () => { void handleGeneratePress(); } },
        ],
      );
    }
  };

  const handleSelectOutfit = async (outfit: Outfit) => {
    setIsSaving(true);
    try {
      const saved = await moodBoardRepository.save(outfit, today);
      setTodayBoard(saved);
      setScreenState('saved');
    } catch {
      Alert.alert(
        'Save Failed',
        'Failed to save moodboard.',
        [
          { text: 'OK' },
          { text: 'Retry', onPress: () => { void handleSelectOutfit(outfit); } },
        ],
      );
    } finally {
      setIsSaving(false);
    }
  };

  // Items available to swap into the current outfit (same category, not already in outfit).
  const swapCandidates = useMemo(() => {
    if (!swapTarget) return [];
    const outfit = outfitOptions[swapTarget.outfitIndex];
    if (!outfit) return [];
    const targetItem = itemMap.get(swapTarget.itemId);
    if (!targetItem) return [];
    const targetZone = classifyZone(targetItem.category);
    const usedIds = new Set(outfit.items);
    return Array.from(itemMap.values()).filter(
      item => !usedIds.has(item.id) && classifyZone(item.category) === targetZone,
    );
  }, [swapTarget, outfitOptions, itemMap]);

  const handleSwapItem = (replacementId: string) => {
    if (!swapTarget) return;
    const updated = [...outfitOptions];
    const outfit = { ...updated[swapTarget.outfitIndex] };
    outfit.items = outfit.items.map(id => (id === swapTarget.itemId ? replacementId : id));
    updated[swapTarget.outfitIndex] = outfit;
    setOutfitOptions(updated);
    setSwapTarget(null);
  };

  const renderContent = () => {
    switch (screenState) {
      case 'loading':
        return (
          <View style={styles.centered}>
            <ActivityIndicator size="large" color={textColor} />
          </View>
        );

      case 'empty':
        return (
          <View style={styles.centered}>
            <Text style={[styles.emptyText, { color: textColor }]}>
              Generate your first{'\n'}mood board
            </Text>
            <GradientButton label="Generate" icon="sunrise" onPress={() => { void handleGeneratePress(); }} />
          </View>
        );

      case 'generating':
        return (
          <View style={styles.centered}>
            <ActivityIndicator size="large" color={textColor} />
            <Text style={[styles.generatingText, { color: textColor }]}>Generating outfits...</Text>
          </View>
        );

      case 'choosing':
        return (
          <View style={styles.choosingContainer}>
            <View
              style={styles.flatListContainer}
              onLayout={e => setCardHeight(e.nativeEvent.layout.height)}
            >
              <FlatList
                data={outfitOptions}
                horizontal
                pagingEnabled
                showsHorizontalScrollIndicator={false}
                keyExtractor={(item) => item.name + '|' + item.items.join(',')}
                onViewableItemsChanged={onViewableItemsChanged}
                viewabilityConfig={viewabilityConfig}
                style={styles.flatList}
                renderItem={({ item, index }) => (
                  <OutfitCard
                    outfit={item}
                    index={index}
                    total={outfitOptions.length}
                    itemMap={itemMap}
                    onSelect={() => { void handleSelectOutfit(item); }}
                    onItemPress={(itemId) => setSwapTarget({ outfitIndex: index, itemId })}
                    isSaving={isSaving}
                    colorScheme={colorScheme}
                    cardHeight={cardHeight}
                    weatherDetail={
                      weather
                        ? {
                            location: weather.location,
                            highTemperature: weather.highTemperature,
                            lowTemperature: weather.lowTemperature,
                            unit: weather.unit,
                          }
                        : undefined
                    }
                  />
                )}
              />
            </View>
            <View style={styles.dotsRow}>
              {outfitOptions.map((_, i) => (
                <View
                  key={i}
                  style={[
                    styles.dot,
                    { backgroundColor: i === activeIndex ? textColor : 'rgba(142,142,147,0.4)' },
                  ]}
                />
              ))}
            </View>
          </View>
        );

      case 'saved':
        if (!todayBoard) return null;
        return (
          <SavedBoardView
            board={todayBoard}
            itemMap={itemMap}
            colorScheme={colorScheme}
            onRegenerate={() => { void handleGeneratePress(); }}
          />
        );

      default:
        return null;
    }
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      <View style={[styles.content, { paddingBottom: tabBottomPadding }]}>
        <View style={styles.mainContent}>
          {renderContent()}
        </View>
      </View>

      {/* Swap item modal */}
      <Modal
        visible={swapTarget !== null}
        title="Swap item"
        onDismiss={() => setSwapTarget(null)}
      >
        {swapTarget && (
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={styles.swapList}
          >
            {swapCandidates.length === 0 ? (
              <Text style={[styles.swapEmpty, { color: labels.tertiary[colorScheme] }]}>
                No alternatives in this category
              </Text>
            ) : (
              swapCandidates.map(candidate => {
                const imgUrl = candidate.pngImageUrl || candidate.imageUrl;
                return (
                  <Pressable
                    key={candidate.id}
                    style={[styles.swapItem, { backgroundColor: fills.tertiary[colorScheme] }]}
                    onPress={() => handleSwapItem(candidate.id)}
                  >
                    {imgUrl ? (
                      <Image
                        source={{ uri: imgUrl }}
                        style={styles.swapImage}
                        contentFit="contain"
                        cachePolicy="memory-disk"
                      />
                    ) : (
                      <Icon name="closet" size={28} color={labels.tertiary[colorScheme]} />
                    )}
                    <Text
                      style={[styles.swapLabel, { color: textColor }]}
                      numberOfLines={2}
                    >
                      {candidate.label}
                    </Text>
                  </Pressable>
                );
              })
            )}
          </ScrollView>
        )}
      </Modal>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: { flex: 1 },
  content: {
    flex: 1,
    paddingHorizontal: CONTAINER_PADDING,
    // Matching top and bottom insets — the card is framed by a consistent
    // dark margin on all sides instead of being flush-top and floating on
    // an uneven bottom gap (pill + safe-area inset).
    paddingTop: 8,
    paddingBottom: 20,
    gap: 16,
  },
  mainContent: { flex: 1 },
  weatherLoading: {
    height: 80,
    justifyContent: 'center',
    alignItems: 'center',
  },

  // Common states
  centered: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    gap: 20,
  },
  emptyText: {
    ...typography.title1.semiBold,
    textAlign: 'center',
  },
  generatingText: {
    ...typography.subheadline.regular,
    textAlign: 'center',
  },

  // Choosing
  choosingContainer: { flex: 1, gap: 8 },
  flatListContainer: { flex: 1 },
  flatList: { flex: 1, marginHorizontal: -CONTAINER_PADDING },

  // Dot indicators
  dotsRow: {
    flexDirection: 'row',
    justifyContent: 'center',
    gap: 6,
  },
  dot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },

  // Swap item modal
  swapList: {
    gap: 12,
    paddingVertical: 8,
  },
  swapItem: {
    width: 90,
    borderRadius: radius.md,
    alignItems: 'center',
    padding: 8,
    gap: 6,
  },
  swapImage: {
    width: 70,
    height: 70,
  },
  swapLabel: {
    ...typography.caption2.regular,
    textAlign: 'center',
  },
  swapEmpty: {
    ...typography.subheadline.regular,
    paddingVertical: 20,
  },
});
