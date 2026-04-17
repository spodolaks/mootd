import {
  GradientIconButton,
  Icon,
  Modal,
} from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, button, fills, grays, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import React, { useState, useCallback, useEffect } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
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

const DARK_GREY = '#3A3A3C';

interface ClothingCardImageProps {
  imageUrl: string;
  pngImageUrl?: string;
  category: string;
  placeholderColor: string;
}

const ClothingCardImage: React.FC<ClothingCardImageProps> = ({ imageUrl, pngImageUrl, category, placeholderColor }) => {
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
      <Text style={[styles.placeholderCategory, { color: placeholderColor }]}>
        {category}
      </Text>
    </View>
  );
};

interface CategoryChip {
  id: string;
  label: string;
  selected?: boolean;
}

const toDetectionSteps = (result: ClothingDetectionResult): DetectionStep[] =>
  result.items.map((item) => ({
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
  const startJob = useDetectionJobStore((s) => s.startJob);
  const jobs = useDetectionJobStore((s) => s.jobs);
  const consumeCompleted = useDetectionJobStore((s) => s.consumeCompleted);
  const dismissJob = useDetectionJobStore((s) => s.dismissJob);
  const showToast = useUIStore((s) => s.showToast);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedCategory, setSelectedCategory] = useState('all');
  const [isAddModalVisible, setIsAddModalVisible] = useState(false);
  const [wardrobeItems, setWardrobeItems] = useState<WardrobeItem[]>([]);
  const [isLoadingItems, setIsLoadingItems] = useState(true);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [isLoadingMore, setIsLoadingMore] = useState(false);

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryTextColor = labels.secondary[colorScheme];
  const searchBgColor = fills.tertiary[colorScheme];
  const placeholderColor = labels.tertiary[colorScheme];
  const cardBgColor = DARK_GREY;
  const chipSelectedBg = button.primary.background[colorScheme];
  const chipSelectedText = button.primary.foreground[colorScheme];
  const cameraBg = button.primary.background[colorScheme];
  const cameraText = button.primary.foreground[colorScheme];
  const uploadBg = button.secondary.background[colorScheme];
  const uploadText = button.secondary.foreground[colorScheme];

  const loadItems = useCallback(async () => {
    setIsLoadingItems(true);
    try {
      const { items, nextCursor: cursor } = await wardrobeRepository.getItems();
      setWardrobeItems(items);
      setNextCursor(cursor);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load wardrobe.';
      Alert.alert('Error', msg);
    } finally {
      setIsLoadingItems(false);
    }
  }, []);

  const loadMore = useCallback(async () => {
    if (!nextCursor || isLoadingMore) return;
    setIsLoadingMore(true);
    try {
      const { items, nextCursor: cursor } = await wardrobeRepository.getItems({ cursor: nextCursor });
      setWardrobeItems(prev => [...prev, ...items]);
      setNextCursor(cursor);
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load more items.';
      Alert.alert('Error', msg);
    } finally {
      setIsLoadingMore(false);
    }
  }, [nextCursor, isLoadingMore]);

  useFocusEffect(
    useCallback(() => {
      void loadItems();
    }, [loadItems]),
  );

  const categories: CategoryChip[] = [
    { id: 'all', label: 'All' },
    ...Array.from(
      new Set(
        wardrobeItems
          .map((i) => i.traits['macro_category'] ?? '')
          .filter(Boolean),
      ),
    ).map((cat) => ({ id: cat, label: cat })),
  ];


  const handleCategoryPress = (categoryId: string) => {
    setSelectedCategory(categoryId);
  };

  const handleAddPress = () => {
    setIsAddModalVisible(true);
  };

  // Watch for completed detection jobs and navigate to review
  useEffect(() => {
    const completed = consumeCompleted();
    if (!completed) return;

    const steps = toDetectionSteps(completed.result!);
    if (steps.length === 0) {
      showToast('No items detected. Try a different photo.', 'error');
      dismissJob(completed.id);
      return;
    }

    showToast(`Detected ${steps.length} item${steps.length === 1 ? '' : 's'} — tap to review`, 'success');
    initializeFlow(steps);
    dismissJob(completed.id);
    router.push('/detected-item');
  }, [jobs, consumeCompleted, dismissJob, initializeFlow, router, showToast]);

  const processImage = useCallback(
    (uri: string) => {
      startJob(uri);
      showToast('Detection started — you can keep browsing', 'info');
    },
    [startJob, showToast],
  );

  const handleCameraPress = useCallback(async () => {
    setIsAddModalVisible(false);
    const { status } = await ImagePicker.requestCameraPermissionsAsync();
    if (status !== 'granted') {
      Alert.alert('Permission required', 'Camera access is needed to take a photo.');
      return;
    }
    const result = await ImagePicker.launchCameraAsync({
      mediaTypes: ['images'],
      allowsEditing: true,
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
      allowsEditing: true,
      quality: 0.8,
    });
    if (!result.canceled && result.assets?.[0]?.uri) {
      processImage(result.assets[0].uri);
    }
  }, [processImage]);

  const handleItemPress = (item: WardrobeItem) => {
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
  };

  const filteredItems = wardrobeItems.filter(item => {
    const matchesSearch = item.label.toLowerCase().includes(searchQuery.toLowerCase());
    const matchesCategory = selectedCategory === 'all' || item.traits['macro_category'] === selectedCategory;
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
        onPress={() => handleCategoryPress(category.id)}
      >
        <Text
          style={[
            styles.categoryChipText,
            { color: isSelected ? chipSelectedText : textColor },
          ]}
        >
          {category.label}
        </Text>
      </Pressable>
    );
  };

  const renderClothingItem = (item: WardrobeItem) => {
    return (
      <Pressable
        key={item.id}
        style={styles.gridItem}
        onPress={() => handleItemPress(item)}
      >
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
  };

  const renderListEmpty = useCallback(() => {
    if (isLoadingItems && wardrobeItems.length === 0) {
      return (
        <View style={styles.centeredState}>
          <ActivityIndicator size="large" color={textColor} />
        </View>
      );
    }
    return (
      <View style={styles.centeredState}>
        <Text style={[styles.emptyText, { color: secondaryTextColor }]}>
          {wardrobeItems.length === 0
            ? 'Your wardrobe is empty.\nTap + to add your first item.'
            : 'No items match your search.'}
        </Text>
      </View>
    );
  }, [isLoadingItems, wardrobeItems.length, textColor, secondaryTextColor]);

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
        contentContainerStyle={styles.categoriesContent}
      >
        {categories.map(renderCategoryChip)}
      </ScrollView>

      {/* Clothing Grid */}
      <FlatList
        data={filteredItems}
        keyExtractor={(item) => item.id}
        numColumns={2}
        renderItem={({ item }) => renderClothingItem(item)}
        ListEmptyComponent={renderListEmpty}
        ListFooterComponent={
          isLoadingMore ? (
            <View style={styles.loadingMoreContainer}>
              <ActivityIndicator size="small" color={textColor} />
            </View>
          ) : null
        }
        onEndReached={() => { void loadMore(); }}
        onEndReachedThreshold={0.5}
        style={styles.gridContainer}
        contentContainerStyle={[styles.gridContent, { paddingBottom: tabBottomPadding }]}
        columnWrapperStyle={styles.gridRow}
        showsVerticalScrollIndicator={false}
      />

      {/* FAB Button — lifted above the floating pill so it doesn't overlap. */}
      <GradientIconButton
        icon="plus"
        size="lg"
        onPress={handleAddPress}
        style={[styles.fab, { bottom: PILL_GUTTER + 8 }]}
      />

      {/* Add Item Modal */}
      <Modal
        visible={isAddModalVisible}
        title="Add item"
        onDismiss={() => setIsAddModalVisible(false)}
        showGrabber
      >
        <View style={styles.modalButtons}>
          <Pressable
            style={[styles.cameraButton, { backgroundColor: cameraBg }]}
            onPress={handleCameraPress}
          >
            <Icon name="camera" size={18} color={cameraText} />
            <Text style={[styles.cameraButtonText, { color: cameraText }]}>Camera</Text>
          </Pressable>

          <Pressable
            style={[styles.uploadButton, { backgroundColor: uploadBg }]}
            onPress={handleUploadPress}
          >
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
