import {
  GradientButton,
  Icon,
  Modal,
} from '@/src/components';
import { useColorScheme, useWeather } from '@/src/hooks';
import { backgrounds, fills, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { radius } from '@/src/theme/radius';
import { wardrobeRepository, moodBoardRepository, feedbackRepository } from '@/src/data/repositories';
import type { Outfit, SavedMoodBoard, WardrobeItem } from '@/src/domain';
import { outfitToSnapshot, topArchetypeOf, weatherContextString } from '@/src/domain';
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

  // Per-outfit thumbs state, immutable-per-render once set. Keyed by the
  // client-assigned outfit.id generated at receive time. Cleared on every
  // fresh generation so the user rates each batch independently.
  const [ratings, setRatings] = useState<Record<string, 'up' | 'down'>>({});
  // Ties every feedback event from this batch back to the generation job, so
  // the training pipeline can reconstruct the (batch → series-of-actions)
  // trajectory for this user.
  const [currentJobId, setCurrentJobId] = useState<string | undefined>(undefined);

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
      let jobId: string | undefined;
      try {
        // Submit async job
        jobId = await wardrobeRepository.submitOutfitGeneration(weatherParams);

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
      // Stamp each outfit with a locally-unique ID so rating and swap
      // feedback events can identify which member of the batch they refer
      // to. Scope is this generation only; IDs aren't persisted beyond the
      // save call. Collisions within a batch are effectively impossible.
      const stamped = outfits.map((o) => ({
        ...o,
        id: o.id ?? `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
      }));
      setItemMap(buildItemMap(items));
      setOutfitOptions(stamped);
      setCurrentJobId(jobId);
      setRatings({});
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
      // Forward the full generatedBatch plus jobId so the server-side
      // feedback emit captures the rejected members of this generation.
      // Without the batch, a saved event records only the pick — training
      // can't reconstruct preference pairs from that alone.
      const saved = await moodBoardRepository.save(outfit, {
        date: today,
        generatedBatch: outfitOptions,
        jobId: currentJobId,
      });
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

    // Emit a feedback event reflecting the swap. The generatedBatch captures
    // the post-swap state of the whole batch (so later training can compare
    // it to the version stored on the "saved" event if the user proceeds to
    // save). Best-effort: we never block the UI on this.
    void feedbackRepository
      .submit({
        action: 'item_swapped',
        jobId: currentJobId,
        chosenOutfitId: outfit.id,
        generatedBatch: updated.map(outfitToSnapshot),
        context: {
          weather: weatherContextString(outfit),
          archetype: topArchetypeOf(outfit),
        },
      })
      .catch((err) => {
        console.warn('[MoodBoard] feedback: item_swapped failed', err);
      });
  };

  const handleRateOutfit = (outfit: Outfit, direction: 'up' | 'down') => {
    if (!outfit.id) return;
    if (ratings[outfit.id]) return; // already rated — immutable per card
    setRatings((prev) => ({ ...prev, [outfit.id!]: direction }));

    void feedbackRepository
      .submit({
        action: 'rated',
        jobId: currentJobId,
        chosenOutfitId: outfit.id,
        rating: direction === 'up' ? 5 : 1,
        generatedBatch: outfitOptions.map(outfitToSnapshot),
        context: {
          weather: weatherContextString(outfit),
          archetype: topArchetypeOf(outfit),
        },
      })
      .catch((err) => {
        console.warn('[MoodBoard] feedback: rated failed', err);
      });
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
                    onThumbsUp={() => handleRateOutfit(item, 'up')}
                    onThumbsDown={() => handleRateOutfit(item, 'down')}
                    rating={item.id ? (ratings[item.id] ?? null) : null}
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
