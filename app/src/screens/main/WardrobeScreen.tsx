import { GradientIconButton, Icon, Modal } from '@/src/components';
import { Skeleton } from '@/src/components/ui';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, button, fills, grays, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import React, { useState, useCallback, useEffect, useRef } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  Pressable,
  View,
} from 'react-native';
import { Image } from 'expo-image';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { useFocusEffect } from '@react-navigation/native';
import * as ImagePicker from 'expo-image-picker';
import { wardrobeRepository } from '@/src/data/repositories';
import { useWardrobeStore, useDetectionJobStore, useUIStore } from '@/src/store';
import type { DetectionStep } from '@/src/store/wardrobeStore';
import type { ClothingDetectionResult, WardrobeItem } from '@/src/domain';
import { useTabContentBottomPadding, PILL_GUTTER } from '@/app/(main)/_layout';

// Wardrobe card backdrop is a solid neutral that gives the cut-out PNG
// items an elevation contrast against the page background. We pick
// per-scheme tokens directly instead of using fills.*, which are
// translucent (rgba) — a transparent backdrop lets the page bleed
// through and washes out the items.
const CARD_BG = {
  light: grays.gray5.light, // #E5E5EA — one step darker than page bg #F2F2F7
  dark: grays.gray4.dark, // #3A3A3C — the pre-existing dark-theme color
};

// F4: stable keyExtractor defined at module scope. The previous inline
// `(item) => item.id` closure allocated fresh on every render, defeating
// FlatList's internal prop-equality check and nudging it toward extra
// reconciliation work.
const wardrobeKey = (item: WardrobeItem): string => item.id;

interface ClothingCardImageProps {
  imageUrl: string;
  pngImageUrl?: string;
  category: string;
  placeholderColor: string;
}

const ClothingCardImage: React.FC<ClothingCardImageProps> = ({
  imageUrl,
  pngImageUrl,
  category,
  placeholderColor,
}) => {
  const [imgError, setImgError] = useState(false);
  // Prefer bg-removed PNG for clean transparent look on the card.
  const displayUrl = pngImageUrl || imageUrl;
  if (displayUrl && !imgError) {
    return (
      <Image
        source={{ uri: displayUrl }}
        style={styles.clothingImage}
        contentFit="contain"
        cachePolicy="memory-disk"
        onError={() => setImgError(true)}
      />
    );
  }
  return (
    <View style={styles.clothingPlaceholder}>
      <Icon name="closet" size={32} color={placeholderColor} />
      <Text style={[styles.placeholderCategory, { color: placeholderColor }]}>{category}</Text>
    </View>
  );
};

interface CategoryChip {
  id: string;
  label: string;
  selected?: boolean;
}

const toDetectionSteps = (result: ClothingDetectionResult): DetectionStep[] =>
  result.items.map(item => ({
    category: item.category,
    similarItems: [
      {
        id: item.id,
        label: item.label,
        imageSource: item.imageUrl ? { uri: item.imageUrl } : undefined,
        traits: item.traits,
      },
    ],
  }));

export const WardrobeScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const tabBottomPadding = useTabContentBottomPadding();
  const router = useRouter();
  const { initializeFlow } = useWardrobeStore();
  const startJob = useDetectionJobStore(s => s.startJob);
  const jobs = useDetectionJobStore(s => s.jobs);
  const consumeCompleted = useDetectionJobStore(s => s.consumeCompleted);
  const consumeFailed = useDetectionJobStore(s => s.consumeFailed);
  const dismissJob = useDetectionJobStore(s => s.dismissJob);
  const showToast = useUIStore(s => s.showToast);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedCategory, setSelectedCategory] = useState('all');
  const [isAddModalVisible, setIsAddModalVisible] = useState(false);
  const [wardrobeItems, setWardrobeItems] = useState<WardrobeItem[]>([]);
  const [isLoadingItems, setIsLoadingItems] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false); // mootd#50 — pull-to-refresh
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [isLoadingMore, setIsLoadingMore] = useState(false);

  // mootd#166 — search + category filtering must span the WHOLE wardrobe,
  // not just the cursor pages loaded so far. We lazily fetch the full set
  // (getAllItems walks every page) the first time a query/filter becomes
  // active and filter client-side over that. The default (no search, "all"
  // category) view stays on the paginated `wardrobeItems` so we don't
  // regress infinite scroll or pay a load-all on mount.
  const [allItems, setAllItems] = useState<WardrobeItem[] | null>(null);
  // Set when a full-wardrobe walk fails, so the trigger effect stops (no
  // auto-retry loop) and the empty state shows a clear message instead of a
  // perpetual spinner. Cleared by loadItems/onRefresh, which re-arms a retry.
  const [allItemsError, setAllItemsError] = useState(false);
  // Concurrency guard for getAllItems (it can walk several pages): a ref so
  // overlapping triggers — e.g. the effect plus a refresh — collapse to one
  // in-flight walk regardless of state-update timing.
  const allInFlight = useRef(false);
  // Guard setState against an unmount mid-fetch and let an in-flight walk
  // no-op its result if the screen has gone away.
  const isMounted = useRef(true);
  useEffect(() => {
    isMounted.current = true;
    return () => {
      isMounted.current = false;
    };
  }, []);

  const isFiltering = searchQuery.trim() !== '' || selectedCategory !== 'all';

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryTextColor = labels.secondary[colorScheme];
  const searchBgColor = fills.tertiary[colorScheme];
  const placeholderColor = labels.tertiary[colorScheme];
  const cardBgColor = CARD_BG[colorScheme];
  const chipSelectedBg = button.primary.background[colorScheme];
  const chipSelectedText = button.primary.foreground[colorScheme];
  const cameraBg = button.primary.background[colorScheme];
  const cameraText = button.primary.foreground[colorScheme];
  const uploadBg = button.secondary.background[colorScheme];
  const uploadText = button.secondary.foreground[colorScheme];

  const loadItems = useCallback(async () => {
    setIsLoadingItems(true);
    // mootd#166 — invalidate the cached full set so the next active
    // search/filter re-fetches it (picks up items added/removed since) and
    // re-arms the trigger effect after a prior full-fetch error.
    setAllItems(null);
    setAllItemsError(false);
    try {
      const { items, nextCursor: cursor } = await wardrobeRepository.getItems();
      if (!isMounted.current) return;
      setWardrobeItems(items);
      setNextCursor(cursor);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load wardrobe.';
      Alert.alert('Error', msg);
    } finally {
      if (isMounted.current) setIsLoadingItems(false);
    }
  }, []);

  // mootd#50 — pull-to-refresh handler. Distinct state from
  // isLoadingItems so the spinner-on-cold-start path stays
  // skeleton-driven; the refresh path keeps existing rows in
  // place and only spins the native pull indicator.
  const onRefresh = useCallback(async () => {
    setIsRefreshing(true);
    // mootd#166 — same invalidation as loadItems so a pull-to-refresh
    // while a search/filter is active re-pulls the full set too (and retries
    // it after a prior failure).
    setAllItems(null);
    setAllItemsError(false);
    try {
      const { items, nextCursor: cursor } = await wardrobeRepository.getItems();
      if (!isMounted.current) return;
      setWardrobeItems(items);
      setNextCursor(cursor);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to refresh wardrobe.';
      Alert.alert('Error', msg);
    } finally {
      if (isMounted.current) setIsRefreshing(false);
    }
  }, []);

  const loadMore = useCallback(async () => {
    if (!nextCursor || isLoadingMore) return;
    setIsLoadingMore(true);
    try {
      const { items, nextCursor: cursor } = await wardrobeRepository.getItems({
        cursor: nextCursor,
      });
      setWardrobeItems(prev => [...prev, ...items]);
      setNextCursor(cursor);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load more items.';
      Alert.alert('Error', msg);
    } finally {
      setIsLoadingMore(false);
    }
  }, [nextCursor, isLoadingMore]);

  // mootd#166 — fetch the complete wardrobe (every cursor page) so search and
  // category filtering operate over all items, not just loaded pages. The
  // allInFlight ref collapses overlapping triggers into one walk. On success
  // `allItems` is set (the trigger effect then sees it non-null and stops);
  // on failure we record allItemsError so the effect stops too — no
  // auto-retry loop. The walk's identity is stable (empty deps), so the
  // trigger effect's deps stay quiet between genuine state transitions.
  const loadAllItems = useCallback(async () => {
    if (allInFlight.current) return;
    allInFlight.current = true;
    setAllItemsError(false);
    try {
      const items = await wardrobeRepository.getAllItems();
      if (!isMounted.current) return;
      setAllItems(items);
    } catch (e) {
      if (!isMounted.current) return;
      setAllItemsError(true);
      const msg = e instanceof Error ? e.message : 'Failed to load wardrobe.';
      Alert.alert('Error', msg);
    } finally {
      allInFlight.current = false;
    }
  }, []);

  // Kick off the full fetch the moment a search query or non-"all" category
  // becomes active and we don't already have (or have failed to fetch) the
  // full set. Excluding the error case prevents an immediate retry loop; a
  // retry is re-armed by loadItems/onRefresh clearing allItemsError, which
  // flips this condition back on.
  useEffect(() => {
    if (isFiltering && allItems === null && !allItemsError) {
      void loadAllItems();
    }
  }, [isFiltering, allItems, allItemsError, loadAllItems]);

  useFocusEffect(
    useCallback(() => {
      void loadItems();
    }, [loadItems])
  );

  // mootd#166 — derive chips from the full set once it's loaded so the row
  // isn't truncated to whatever pages happen to be in memory. Falls back to
  // the loaded pages until the full walk completes.
  const categorySource = allItems ?? wardrobeItems;
  const categories: CategoryChip[] = [
    { id: 'all', label: 'All' },
    // Group by the item's canonical category. Fall back to the legacy
    // `macro_category` trait for rows created by the old detector, which set
    // that trait instead of populating the top-level category.
    ...Array.from(
      new Set(
        categorySource.map(i => i.category || i.traits['macro_category'] || '').filter(Boolean)
      )
    ).map(cat => ({ id: cat, label: cat })),
  ];

  const handleCategoryPress = (categoryId: string) => {
    setSelectedCategory(categoryId);
  };

  const handleAddPress = () => {
    setIsAddModalVisible(true);
  };

  const processImage = useCallback(
    (uri: string) => {
      startJob(uri);
      showToast('Detection started — you can keep browsing', 'info');
    },
    [startJob, showToast]
  );

  // Watch for completed/failed detection jobs. Completed jobs route to the
  // review flow; failed/timed-out jobs surface an alert with a Retry that
  // re-runs detection from the stored image URI. Both branches dismiss the
  // job afterwards so it doesn't re-fire on the next render.
  useEffect(() => {
    const completed = consumeCompleted();
    if (completed) {
      const steps = toDetectionSteps(completed.result!);
      if (steps.length === 0) {
        showToast('No items detected. Try a different photo.', 'error');
        dismissJob(completed.id);
        return;
      }

      showToast(
        `Detected ${steps.length} item${steps.length === 1 ? '' : 's'} — tap to review`,
        'success'
      );
      initializeFlow(steps);
      dismissJob(completed.id);
      router.push('/detected-item');
      return;
    }

    const failed = consumeFailed();
    if (failed) {
      // Dismiss first so the alert's Retry (which enqueues a fresh job) can't
      // be re-consumed by this same failed entry on the next render.
      dismissJob(failed.id);
      Alert.alert('Detection failed', failed.error || 'We couldn’t detect clothing in that photo.', [
        { text: 'Dismiss', style: 'cancel' },
        { text: 'Retry', onPress: () => processImage(failed.imageUri) },
      ]);
    }
  }, [jobs, consumeCompleted, consumeFailed, dismissJob, initializeFlow, router, showToast, processImage]);

  const handleCameraPress = useCallback(async () => {
    setIsAddModalVisible(false);
    const { status } = await ImagePicker.requestCameraPermissionsAsync();
    if (status !== 'granted') {
      Alert.alert('Permission required', 'Camera access is needed to take a photo.');
      return;
    }
    const result = await ImagePicker.launchCameraAsync({
      mediaTypes: ['images'],
      // Skip the iOS square-crop editor: we want the full-resolution frame
      // so the clothing detector has as many pixels as possible to work with.
      quality: 0.8,
    });
    if (!result.canceled && result.assets?.[0]?.uri) {
      processImage(result.assets[0].uri);
    }
  }, [processImage]);

  const handleUploadPress = useCallback(async () => {
    setIsAddModalVisible(false);
    const { status } = await ImagePicker.requestMediaLibraryPermissionsAsync();
    if (status !== 'granted') {
      Alert.alert('Permission required', 'Photo library access is needed.');
      return;
    }
    const result = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ['images'],
      // Same reason — keep the original photo instead of the cropped square.
      quality: 0.8,
    });
    if (!result.canceled && result.assets?.[0]?.uri) {
      processImage(result.assets[0].uri);
    }
  }, [processImage]);

  const handleItemPress = useCallback(
    (item: WardrobeItem) => {
      router.push({
        pathname: '/item-details',
        params: {
          id: item.id,
          name: item.label,
          category: item.category,
          imageUrl: item.pngImageUrl || item.imageUrl,
          traits: JSON.stringify(item.traits ?? {}),
        },
      });
    },
    [router]
  );

  // mootd#166 — when a search/filter is active, run the predicate over the
  // FULL wardrobe (allItems) once it's loaded; until then fall back to the
  // loaded pages so the user sees partial matches rather than an empty grid.
  // With no search/filter, stay on the paginated list to preserve infinite
  // scroll. `displaySource` is the same array `filteredItems` filters, so the
  // empty-state logic can tell a "no matches over the full set" apart from a
  // "still loading the full set".
  const displaySource = isFiltering ? (allItems ?? wardrobeItems) : wardrobeItems;
  const filteredItems = displaySource.filter(item => {
    const matchesSearch = item.label.toLowerCase().includes(searchQuery.toLowerCase());
    const matchesCategory =
      selectedCategory === 'all' ||
      (item.category || item.traits['macro_category']) === selectedCategory;
    return matchesSearch && matchesCategory;
  });

  const renderCategoryChip = (category: CategoryChip) => {
    const isSelected = selectedCategory === category.id;
    return (
      <Pressable
        key={category.id}
        style={[
          styles.categoryChip,
          { backgroundColor: isSelected ? chipSelectedBg : searchBgColor },
        ]}
        onPress={() => handleCategoryPress(category.id)}>
        <Text
          style={[styles.categoryChipText, { color: isSelected ? chipSelectedText : textColor }]}>
          {category.label}
        </Text>
      </Pressable>
    );
  };

  // F4: wrap in useCallback so FlatList sees a stable renderItem across
  // re-renders, avoiding its internal recycler tearing down visible
  // rows when the parent re-renders for unrelated reasons (scroll
  // position change, tab focus, loading-more flip).
  const renderClothingItem = useCallback(
    ({ item }: { item: WardrobeItem }) => {
      return (
        <Pressable
          key={item.id}
          style={styles.gridItem}
          onPress={() => handleItemPress(item)}
          testID={`wardrobe-item-${item.id}`}
          accessibilityRole="button"
          accessibilityLabel={`Open ${item.label}`}>
          <View style={[styles.clothingCard, { backgroundColor: cardBgColor }]}>
            <ClothingCardImage
              imageUrl={item.imageUrl}
              pngImageUrl={item.pngImageUrl}
              category={item.category}
              placeholderColor={placeholderColor}
            />
          </View>
          <Text style={[styles.itemName, { color: secondaryTextColor }]}>{item.label}</Text>
        </Pressable>
      );
    },
    [cardBgColor, placeholderColor, secondaryTextColor, handleItemPress]
  );

  const renderListEmpty = useCallback(() => {
    if (isLoadingItems && wardrobeItems.length === 0) {
      // mootd#50 — six skeleton cards on initial load. Same
      // grid layout as the real cards (numColumns=2) so
      // there's no jump when data arrives.
      return (
        <View style={styles.skeletonGrid}>
          {[0, 1, 2, 3, 4, 5].map(i => (
            <View key={i} style={styles.skeletonItem}>
              <Skeleton style={styles.skeletonImage} />
              <Skeleton style={styles.skeletonLabel} />
            </View>
          ))}
        </View>
      );
    }
    // mootd#166 — a full-wardrobe walk failed while filtering. Don't claim
    // "no matches" (we never finished searching); tell the user and point at
    // pull-to-refresh, which re-arms the fetch.
    if (isFiltering && allItemsError) {
      return (
        <View style={styles.centeredState}>
          <Text style={[styles.emptyText, { color: secondaryTextColor }]}>
            {'Couldn’t search your full wardrobe.\nPull down to retry.'}
          </Text>
        </View>
      );
    }
    // mootd#166 — while a search/filter is active and the full wardrobe hasn't
    // loaded yet, show a spinner instead of "No items match". A false negative
    // here is exactly the bug we're fixing: the match may live on a page that
    // hasn't been walked in yet. Covers the brief pre-fetch tick too (allItems
    // null, not yet loading) so there's no flash of "No items match".
    if (isFiltering && allItems === null) {
      return (
        <View style={styles.centeredState}>
          <ActivityIndicator size="small" color={textColor} />
          <Text style={[styles.emptyText, { color: secondaryTextColor, marginTop: 12 }]}>
            Searching your wardrobe…
          </Text>
        </View>
      );
    }
    return (
      <View style={styles.centeredState}>
        <Text style={[styles.emptyText, { color: secondaryTextColor }]}>
          {isFiltering
            ? 'No items match your search.'
            : 'Your wardrobe is empty.\nTap + to add your first item.'}
        </Text>
      </View>
    );
  }, [
    isLoadingItems,
    wardrobeItems.length,
    isFiltering,
    allItems,
    allItemsError,
    textColor,
    secondaryTextColor,
  ]);

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      {/* Header */}
      <View style={styles.header}>
        <Text style={[styles.title, { color: textColor }]}>Wardrobe</Text>
      </View>

      {/* Search Bar */}
      <View style={styles.searchContainer}>
        <View style={[styles.searchBar, { backgroundColor: searchBgColor }]}>
          <Icon name="search" size={20} color={placeholderColor} />
          <TextInput
            style={[styles.searchInput, { color: textColor }]}
            placeholder="Search"
            placeholderTextColor={placeholderColor}
            value={searchQuery}
            onChangeText={setSearchQuery}
          />
        </View>
      </View>

      {/* Category Filter */}
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.categoriesContainer}
        contentContainerStyle={styles.categoriesContent}>
        {categories.map(renderCategoryChip)}
      </ScrollView>

      {/* Clothing Grid — F4 virtualisation tuning for large wardrobes. */}
      <FlatList
        data={filteredItems}
        keyExtractor={wardrobeKey}
        numColumns={2}
        renderItem={renderClothingItem}
        ListEmptyComponent={renderListEmpty}
        ListFooterComponent={
          // mootd#166 — the load-more spinner belongs to the paginated
          // (unfiltered) list only; the filtered view runs over the full set.
          !isFiltering && isLoadingMore ? (
            <View style={styles.loadingMoreContainer}>
              <ActivityIndicator size="small" color={textColor} />
            </View>
          ) : null
        }
        // mootd#50 — pull-to-refresh.
        refreshControl={
          <RefreshControl
            refreshing={isRefreshing}
            onRefresh={() => {
              void onRefresh();
            }}
            tintColor={textColor}
          />
        }
        onEndReached={() => {
          // mootd#166 — cursor pagination drives the default view only. When
          // a search/filter is active we're already filtering the full set,
          // so paging in more cursor pages would be wasted work.
          if (!isFiltering) void loadMore();
        }}
        onEndReachedThreshold={0.5}
        style={styles.gridContainer}
        contentContainerStyle={[styles.gridContent, { paddingBottom: tabBottomPadding }]}
        columnWrapperStyle={styles.gridRow}
        showsVerticalScrollIndicator={false}
        // F4 virtualisation: default windowSize=21 is ~10 viewports above
        // and below — huge waste for a grid of image-heavy cards. Tighten
        // to 5 (current + 2 on each side) so offscreen cards unmount and
        // free their image memory. maxToRenderPerBatch=10 caps per-frame
        // render work so a long list can't starve the UI thread during
        // initial layout. initialNumToRender=10 shows a full first
        // viewport without over-rendering. removeClippedSubviews is
        // forced true (Android default false) so the same offscreen-
        // mount-detach rule applies cross-platform.
        windowSize={5}
        maxToRenderPerBatch={10}
        initialNumToRender={10}
        removeClippedSubviews
      />

      {/* FAB Button — lifted above the floating pill so it doesn't overlap. */}
      <GradientIconButton
        icon="plus"
        size="lg"
        onPress={handleAddPress}
        style={[styles.fab, { bottom: PILL_GUTTER + 8 }]}
        testID="wardrobe-add-item"
        accessibilityLabel="Add a new wardrobe item"
      />

      {/* Add Item Modal */}
      <Modal
        visible={isAddModalVisible}
        title="Add item"
        onDismiss={() => setIsAddModalVisible(false)}
        showGrabber>
        <View style={styles.modalButtons}>
          <Pressable
            style={[styles.cameraButton, { backgroundColor: cameraBg }]}
            onPress={handleCameraPress}>
            <Icon name="camera" size={18} color={cameraText} />
            <Text style={[styles.cameraButtonText, { color: cameraText }]}>Camera</Text>
          </Pressable>

          <Pressable
            style={[styles.uploadButton, { backgroundColor: uploadBg }]}
            onPress={handleUploadPress}>
            <Icon name="upload" size={18} color={uploadText} />
            <Text style={[styles.uploadButtonText, { color: uploadText }]}>Upload</Text>
          </Pressable>
        </View>
      </Modal>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  header: {
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 16,
  },
  title: {
    ...typography.largeTitle.semiBold,
  },
  searchContainer: {
    paddingHorizontal: 16,
    marginBottom: 16,
  },
  searchBar: {
    flexDirection: 'row',
    alignItems: 'center',
    height: 56,
    borderRadius: 16,
    paddingHorizontal: 16,
    gap: 12,
  },
  searchInput: {
    flex: 1,
    ...typography.body.regular,
    height: '100%',
  },
  categoriesContainer: {
    flexGrow: 0,
    marginBottom: 16,
  },
  categoriesContent: {
    paddingHorizontal: 16,
    gap: 8,
  },
  categoryChip: {
    height: 28,
    paddingHorizontal: 14,
    borderRadius: 14,
    justifyContent: 'center',
    alignItems: 'center',
    marginRight: 8,
  },
  categoryChipText: {
    ...typography.footnote.semiBold,
  },
  gridContainer: {
    flex: 1,
  },
  gridContent: {
    paddingHorizontal: 15,
    paddingBottom: 100,
    gap: 16,
  },
  gridRow: {
    flexDirection: 'row',
    gap: 8,
  },
  gridItem: {
    flex: 1,
    gap: 8,
  },
  clothingCard: {
    aspectRatio: 3 / 4,
    borderRadius: 24,
    overflow: 'hidden',
  },
  clothingImage: {
    width: '100%',
    height: '100%',
  },
  clothingPlaceholder: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    gap: 8,
  },
  placeholderCategory: {
    ...typography.caption1.regular,
    textTransform: 'capitalize',
  },
  loadingMoreContainer: {
    paddingVertical: 16,
    alignItems: 'center',
  },
  centeredState: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingTop: 60,
  },
  // mootd#50 — skeleton placeholders during initial load. Match
  // the real-card grid (2 cols, square image + caption below)
  // so there's no layout jump when data arrives.
  skeletonGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    paddingHorizontal: 16,
    gap: 12,
  },
  skeletonItem: {
    width: '47%',
    gap: 6,
  },
  skeletonImage: {
    aspectRatio: 1,
    borderRadius: 12,
  },
  skeletonLabel: {
    height: 12,
    width: '70%',
    alignSelf: 'center',
    borderRadius: 4,
  },
  emptyText: {
    ...typography.subheadline.regular,
    textAlign: 'center',
    lineHeight: 22,
  },
  itemName: {
    ...typography.caption1.regular,
    textAlign: 'center',
  },
  fab: {
    position: 'absolute',
    bottom: 15,
    right: 15,
  },
  modalButtons: {
    gap: 8,
  },
  cameraButton: {
    height: 54,
    borderRadius: 27,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
  },
  cameraButtonText: {
    ...typography.body.semiBold,
  },
  uploadButton: {
    height: 54,
    borderRadius: 27,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
  },
  uploadButtonText: {
    ...typography.body.semiBold,
  },
});
