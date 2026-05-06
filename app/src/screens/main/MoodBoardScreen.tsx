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
import type { CollageCaptureHandle } from '@/src/components/moodboard/OutfitCard';
import { SavedBoardView } from '@/src/components/moodboard/SavedBoardView';
import { SCREEN_WIDTH, CONTAINER_PADDING } from '@/src/components/moodboard/constants';
import { useTabContentBottomPadding } from '@/app/(main)/_layout';

type ScreenState = 'loading' | 'empty' | 'generating' | 'choosing' | 'saved';

const buildItemMap = (items: WardrobeItem[]): Map<string, WardrobeItem> => {
  const map = new Map<string, WardrobeItem>();
  items.forEach(item => map.set(item.id, item));
  return map;
};

// Stable FlatList keyExtractor. Prefer the client-assigned outfit.id
// (unique per batch, stable across re-renders); fall back to a
// name+items fingerprint for any legacy path without an id.
const outfitKey = (o: Outfit): string =>
  o.id ?? `${o.name}|${o.items.join(',')}`;

export const MoodBoardScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const [screenState, setScreenState] = useState<ScreenState>('loading');
  const [outfitOptions, setOutfitOptions] = useState<Outfit[]>([]);
  const [todayBoard, setTodayBoard] = useState<SavedMoodBoard | null>(null);
  const [itemMap, setItemMap] = useState<Map<string, WardrobeItem>>(new Map());
  const [isSaving, setIsSaving] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const [cardHeight, setCardHeight] = useState(0);
  // mootd#62 — progress message during streaming generation.
  // Updated by the SSE callback so the user sees "Drafting
  // outfits…" / "Almost there…" instead of a static spinner.
  const [progressMessage, setProgressMessage] = useState<string | null>(null);

  // Swap item modal state
  const [swapTarget, setSwapTarget] = useState<{ outfitIndex: number; itemId: string } | null>(null);

  // Per-outfit thumbs state, immutable-per-render once set. Keyed by the
  // client-assigned outfit.id generated at receive time. Cleared on every
  // fresh generation so the user rates each batch independently.
  const [ratings, setRatings] = useState<Record<string, 'up' | 'down'>>({});
  // One capture handle per card in the batch, indexed by the outfit's
  // client ID. Populated by OutfitCard via its imperative handle; read at
  // Save time. handle.capture() hides the native/web split and returns a
  // ready-to-send "data:image/png;base64,…" string (or null on failure, in
  // which case the save still proceeds — just without a render).
  const collageCaptureRefs = useRef<Record<string, CollageCaptureHandle | null>>({});
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
  const onViewableItemsChanged = useRef(({ viewableItems }: { viewableItems: { index: number | null }[] }) => {
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
    setProgressMessage(null);
    try {
      const weatherParams = weather
        ? { temperature: weather.temperature, condition: weather.condition, unit: weather.unit }
        : undefined;

      // mootd#42 — mint one Idempotency-Key per Generate press
      // and reuse it across this attempt's network retries. The
      // key is short-lived (backend TTL = 60s) so re-using a
      // press's key won't collide with a future, intentional
      // re-generation. UUID-grade uniqueness isn't required —
      // userId + ts + random bits is enough for the dedupe
      // window.
      const idempotencyKey = `gen-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;

      let outfits: Outfit[];
      let jobId: string | undefined;
      try {
        // mootd#62 — prefer the streaming path when the repo
        // implements it. Surfaces per-stage progress messages
        // ("Drafting outfits…" etc.) while the LLM call is in
        // flight, so the user sees activity instead of a blank
        // spinner. Polling fallback path stays intact for
        // implementations without streaming.
        if (wardrobeRepository.streamOutfitGeneration) {
          outfits = await wardrobeRepository.streamOutfitGeneration(
            (progress) => {
              if (progress.description) {
                setProgressMessage(progress.description);
              }
            },
            weatherParams,
            idempotencyKey,
          );
        } else {
          jobId = await wardrobeRepository.submitOutfitGeneration(weatherParams, idempotencyKey);
          let result: { status: string; outfits?: Outfit[]; error?: string };
          do {
            await new Promise(resolve => setTimeout(resolve, 2000));
            result = await wardrobeRepository.pollOutfitJob(jobId);
          } while (result.status === 'pending' || result.status === 'processing');
          if (result.status === 'failed') {
            throw new Error(result.error || 'Outfit generation failed');
          }
          outfits = result.outfits ?? [];
        }
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
      // Discard stale refs from the previous batch so we don't accidentally
      // capture an old card's handle on the next Save.
      collageCaptureRefs.current = {};
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

  const handleSelectOutfit = useCallback(async (outfit: Outfit) => {
    setIsSaving(true);
    try {
      // Capture a PNG of the collage exactly as the user sees it — this is
      // what the calendar will render as the "hero" image for the saved
      // moodboard. The handle abstracts the native/web split. Best-effort:
      // null on failure means we just send the save without boardImage and
      // the calendar falls back to rendering from snapshots.
      let boardImage: string | undefined;
      const handle = outfit.id ? collageCaptureRefs.current[outfit.id] : null;
      if (handle) {
        const captured = await handle.capture();
        boardImage = captured ?? undefined;
      }

      // Forward the full generatedBatch plus jobId so the server-side
      // feedback emit captures the rejected members of this generation.
      // Without the batch, a saved event records only the pick — training
      // can't reconstruct preference pairs from that alone.
      const saved = await moodBoardRepository.save(outfit, {
        date: today,
        generatedBatch: outfitOptions,
        jobId: currentJobId,
        boardImage,
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
  }, [outfitOptions, today, currentJobId]);

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
    const removedItemId = swapTarget.itemId;
    const updated = [...outfitOptions];
    const outfit = { ...updated[swapTarget.outfitIndex] };
    outfit.items = outfit.items.map(id => (id === removedItemId ? replacementId : id));
    updated[swapTarget.outfitIndex] = outfit;
    setOutfitOptions(updated);
    setSwapTarget(null);

    // Emit a feedback event reflecting the swap. swappedFrom / swappedTo
    // give training the explicit (rejected → accepted) pair without having
    // to diff sequential generatedBatch snapshots; the post-swap
    // generatedBatch is still included so the full state is preserved.
    // Best-effort: we never block the UI on this.
    void feedbackRepository
      .submit({
        action: 'item_swapped',
        jobId: currentJobId,
        chosenOutfitId: outfit.id,
        swappedFrom: removedItemId,
        swappedTo: replacementId,
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

  const handleRateOutfit = useCallback((outfit: Outfit, direction: 'up' | 'down') => {
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
  }, [ratings, currentJobId, outfitOptions]);

  // F2: stabilise renderItem so FlatList doesn't tear down + recreate
  // the card tree on every parent render. Combined with OutfitCard's
  // React.memo + propsAreEqual, unrelated state updates (isSaving,
  // swapTarget, rating flicker, cardHeight) stop forcing all 3–5
  // cards to re-render their Collage subtree. The setSwapTarget,
  // collageCaptureRefs.current = {...}, and weather props are all
  // stable via useRef / useCallback / constant references.
  const weatherDetail = useMemo(() =>
    weather
      ? {
          location: weather.location,
          highTemperature: weather.highTemperature,
          lowTemperature: weather.lowTemperature,
          unit: weather.unit,
        }
      : undefined,
    [weather],
  );

  const renderOutfitCard = useCallback(({ item, index }: { item: Outfit; index: number }) => (
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
      weatherDetail={weatherDetail}
      onThumbsUp={() => handleRateOutfit(item, 'up')}
      onThumbsDown={() => handleRateOutfit(item, 'down')}
      rating={item.id ? (ratings[item.id] ?? null) : null}
      collageCaptureRef={item.id
        ? (handle) => {
            if (item.id) {
              collageCaptureRefs.current[item.id] = handle;
            }
          }
        : undefined}
    />
  ), [outfitOptions.length, itemMap, handleSelectOutfit, isSaving, colorScheme, cardHeight, weatherDetail, handleRateOutfit, ratings]);

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
            <GradientButton
              label="Generate"
              icon="sunrise"
              onPress={() => { void handleGeneratePress(); }}
              testID="moodboard-generate"
              accessibilityLabel="Generate outfits"
            />
          </View>
        );

      case 'generating':
        return (
          <View style={styles.centered}>
            <ActivityIndicator size="large" color={textColor} />
            <Text style={[styles.generatingText, { color: textColor }]}>
              {/* mootd#62 — show the live progress message
                  when streaming is wired; static fallback when
                  the backend reports no progress (older builds). */}
              {progressMessage ?? 'Generating outfits...'}
            </Text>
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
                // Use the client-assigned outfit ID when present — stable
                // across re-renders, unique per batch. Fall back to a
                // name+items fingerprint for legacy/pre-id paths.
                keyExtractor={outfitKey}
                onViewableItemsChanged={onViewableItemsChanged}
                viewabilityConfig={viewabilityConfig}
                style={styles.flatList}
                renderItem={renderOutfitCard}
              />
            </View>
            <View style={styles.dotsRow}>
              {outfitOptions.map((_, i) => (
                // #23 — inactive dot uses fills.tertiary so it follows
                // the theme in dark mode instead of staying at a fixed
                // rgba grey that reads too opaque against dark surfaces.
                // Active dot continues to use textColor for maximum
                // contrast against the tertiary fill.
                <View
                  key={i}
                  style={[
                    styles.dot,
                    { backgroundColor: i === activeIndex ? textColor : fills.tertiary[colorScheme] },
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
                  // #22 — the swap candidate tiles are the decision
                  // point for users picking a replacement garment; they
                  // need a screen-reader label that names the item, not
                  // just "button". Use the candidate's label and a hint
                  // that explains what the tap will do.
                  <Pressable
                    key={candidate.id}
                    style={[styles.swapItem, { backgroundColor: fills.tertiary[colorScheme] }]}
                    onPress={() => handleSwapItem(candidate.id)}
                    accessibilityRole="button"
                    accessibilityLabel={`Swap with ${candidate.label}`}
                    accessibilityHint="Replaces the selected garment in this outfit"
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
    // paddingTop is 0 because SafeAreaView already adds the full device-top
    // inset (notch / Dynamic Island). Adding more pushed the card visibly
    // low on iPhones with large top insets without improving the margin.
    // The card's own 16px internal padding gives the content its breathing
    // room; the pill at the bottom uses a dedicated padding so the two
    // sides aren't expected to match visually.
    paddingTop: 0,
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
