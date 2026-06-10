import { BrandSearchInput, Button, Text } from '@/src/components';
import { ClothingItemCard } from '@/src/components/ui/ClothingItemCard';
import { SegmentedProgressBar } from '@/src/components/ui/SegmentedProgressBar';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, labels } from '@/src/theme/colors';
import { MOCK_DETECTION_STEPS } from '@/src/data/mock';
import { brandsRepository } from '@/src/data/repositories';
import type { ClothingSearchProduct } from '@/src/domain';
import React, { useState } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, View } from 'react-native';
import type { ImageSourcePropType } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

interface DetectedItem {
  id: string;
  label: string;
  imageSource?: ImageSourcePropType;
  hasPng?: boolean;
}

interface DetectedStepData {
  category: string;
  similarItems: DetectedItem[];
}

interface DetectedItemScreenProps {
  /**
   * Array of detected items data for each step
   */
  steps?: DetectedStepData[];
  /**
   * Override the current step index for progress display
   */
  currentStepOverride?: number;
  /**
   * Override the total steps for progress display
   */
  totalStepsOverride?: number;
  /**
   * Initial selections to pre-populate (for when navigating back)
   */
  initialSelections?: Record<number, string>;
  /**
   * Callback when user exits the flow (back on first step)
   */
  onExit?: () => void;
  /**
   * Callback when all items are confirmed with selections.
   * brand is the value entered in the brand input for the current step.
   */
  onComplete?: (selections: Record<number, string>, brand: string) => void;
  /**
   * Called when user enters a brand; should call the search service and return results.
   * itemId is the wardrobe item ID of the first similar item for the current step.
   */
  onBrandSearch?: (itemId: string, brand: string) => Promise<ClothingSearchProduct[]>;
  /**
   * Called when user selects a search result product instead of a detected item.
   */
  onProductSelect?: (product: ClothingSearchProduct, detectedItemId: string, brand: string) => void;
}

const DEFAULT_STEPS: DetectedStepData[] = MOCK_DETECTION_STEPS;

export const DetectedItemScreen: React.FC<DetectedItemScreenProps> = ({
  steps = DEFAULT_STEPS,
  currentStepOverride,
  totalStepsOverride,
  initialSelections = {},
  onExit,
  onComplete,
  onBrandSearch,
  onProductSelect,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const [currentStep, setCurrentStep] = useState(0);
  const [selections, setSelections] = useState<Record<number, string>>(initialSelections);
  const [brand, setBrand] = useState('');
  const [searchProducts, setSearchProducts] = useState<ClothingSearchProduct[] | null>(null);
  const [isSearching, setIsSearching] = useState(false);
  // Distinguishes a failed search (network/error) from a successful search
  // that returned zero results. Without this the catch coerced errors into
  // an empty array, silently showing "no results" for a real failure.
  const [searchError, setSearchError] = useState(false);
  const [selectedProductId, setSelectedProductId] = useState<string | null>(null);

  const totalItems = steps.length;
  const currentStepData = steps[currentStep];
  const similarItems = currentStepData?.similarItems ?? [];
  const detectedCategory = currentStepData?.category ?? '';
  const selectedItemId = selections[currentStep] ?? null;
  const isLastStep = currentStep === totalItems - 1;
  const isFirstStep = currentStep === 0;
  const hasSelection = selectedItemId !== null || selectedProductId !== null;

  // Use overrides for progress display if provided
  const displayCurrentStep = currentStepOverride ?? currentStep;
  const displayTotalSteps = totalStepsOverride ?? totalItems;

  const backgroundColor = backgrounds.primary[colorScheme];
  const secondaryTextColor = labels.tertiary[colorScheme];

  const handleBack = () => {
    if (isFirstStep) {
      onExit?.();
    } else {
      setCurrentStep(prev => prev - 1);
      setSearchProducts(null);
      setSearchError(false);
      setSelectedProductId(null);
      setBrand('');
    }
  };

  const handleNext = () => {
    if (selectedProductId !== null && searchProducts) {
      const product = searchProducts.find(p => p.id === selectedProductId);
      const detectedItemId = similarItems[0]?.id ?? '';
      if (product) {
        onProductSelect?.(product, detectedItemId, brand.trim());
        return;
      }
    }
    if (isLastStep) {
      onComplete?.(selections, brand);
    } else {
      setCurrentStep(prev => prev + 1);
      setSearchProducts(null);
      setSearchError(false);
      setSelectedProductId(null);
      setBrand('');
    }
  };

  const handleItemSelect = (itemId: string) => {
    setSelectedProductId(null);
    setSelections(prev => ({
      ...prev,
      [currentStep]: itemId,
    }));
  };

  const handleProductSelect = (productId: string) => {
    setSelectedProductId(productId);
    setSelections(prev => {
      const updated = { ...prev };
      delete updated[currentStep];
      return updated;
    });
  };

  const handleBrandSearch = async () => {
    const trimmed = brand.trim();
    if (trimmed) {
      void brandsRepository.saveBrand(trimmed);
    }

    if (!trimmed || !onBrandSearch || isSearching) return;
    const itemId = similarItems[0]?.id;
    if (!itemId) return;

    setIsSearching(true);
    setSearchProducts(null);
    setSearchError(false);
    try {
      const results = await onBrandSearch(itemId, trimmed);
      setSearchProducts(results);
    } catch {
      // Keep the error distinct from a zero-result success so the UI can show
      // "Search failed — try again." instead of silently rendering nothing.
      setSearchProducts(null);
      setSearchError(true);
    } finally {
      setIsSearching(false);
    }
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top', 'bottom']}>
      <View style={styles.content}>
        {/* Progress Bar */}
        <SegmentedProgressBar
          totalSegments={displayTotalSteps}
          currentSegment={displayCurrentStep}
          style={styles.progressBar}
        />

        {/* Item Counter */}
        <Text variant="body" style={[styles.itemCounter, { color: secondaryTextColor }]}>
          Item {displayCurrentStep + 1} of {displayTotalSteps}
        </Text>

        {/* Title Section */}
        <View style={styles.titleSection}>
          <Text variant="title1" weight="semiBold" style={styles.title}>
            We detected your {detectedCategory}
          </Text>
        </View>

        {/* Brand Search */}
        <BrandSearchInput
          value={brand}
          onChangeText={text => {
            setBrand(text);
            if (!text.trim()) {
              setSearchProducts(null);
              setSearchError(false);
            }
          }}
          onBlur={() => {
            void handleBrandSearch();
          }}
          onSubmitEditing={() => {
            void handleBrandSearch();
          }}
          placeholder="Search brand…"
          style={styles.brandInput}
        />

        <ScrollView
          style={styles.scrollView}
          contentContainerStyle={styles.scrollContent}
          showsVerticalScrollIndicator={false}>
          {/* Section header */}
          <View style={styles.subtitleSection}>
            <Text variant="body" weight="semiBold" style={styles.subtitle}>
              Which one matches best?
            </Text>
            <Text variant="subheadline" style={[styles.description, { color: secondaryTextColor }]}>
              Select the closest match to your item
            </Text>
          </View>

          {/* Search status label: searching / error / result count (incl. zero) */}
          {(isSearching || searchError || searchProducts !== null) && (
            <Text
              variant="subheadline"
              weight="semiBold"
              style={[styles.searchResultsLabel, { color: secondaryTextColor }]}>
              {isSearching
                ? 'Searching…'
                : searchError
                  ? 'Search failed — try again.'
                  : `${searchProducts!.length} result${searchProducts!.length !== 1 ? 's' : ''} for "${brand.trim()}"`}
            </Text>
          )}

          {/* Unified grid: detected items + search products */}
          <View style={styles.grid}>
            {similarItems.map(item => (
              <View key={item.id} style={styles.gridItem}>
                <ClothingItemCard
                  label={item.label}
                  selected={selectedItemId === item.id}
                  imageSource={item.imageSource}
                  darkBackground={item.hasPng}
                  onPress={() => handleItemSelect(item.id)}
                />
              </View>
            ))}

            {isSearching && (
              <View style={styles.gridItem}>
                <View style={styles.searchingCard}>
                  <ActivityIndicator size="small" color={secondaryTextColor} />
                </View>
              </View>
            )}

            {!isSearching &&
              searchProducts?.map(product => (
                <View key={product.id} style={styles.gridItem}>
                  <ClothingItemCard
                    label={product.title}
                    selected={selectedProductId === product.id}
                    imageSource={product.imageUrl ? { uri: product.imageUrl } : undefined}
                    darkBackground={false}
                    onPress={() => handleProductSelect(product.id)}
                  />
                  {product.source || product.price ? (
                    <View style={styles.productMeta}>
                      {product.source ? (
                        <Text
                          variant="caption2"
                          numberOfLines={1}
                          style={[styles.productMetaText, { color: secondaryTextColor }]}>
                          {product.source}
                        </Text>
                      ) : null}
                      {product.price ? (
                        <Text
                          variant="caption2"
                          style={[styles.productMetaText, { color: secondaryTextColor }]}>
                          {product.price}
                        </Text>
                      ) : null}
                    </View>
                  ) : null}
                </View>
              ))}
          </View>
        </ScrollView>

        {/* Bottom Buttons */}
        <View style={styles.buttonsContainer}>
          <Button
            label="Back"
            variant="secondary"
            size="lg"
            onPress={handleBack}
            style={styles.backButton}
          />
          <Button
            label={isLastStep ? 'Done' : 'Next'}
            variant="primary"
            size="lg"
            onPress={handleNext}
            disabled={!hasSelection}
            style={styles.nextButton}
          />
        </View>
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
    marginTop: 8,
  },
  itemCounter: {
    textAlign: 'center',
    marginTop: 24,
    marginBottom: 16,
  },
  titleSection: {
    marginBottom: 16,
  },
  title: {
    textAlign: 'center',
  },
  brandInput: {
    marginTop: 16,
    marginBottom: 8,
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {
    paddingBottom: 16,
  },

  searchResultsLabel: {
    marginBottom: 12,
    marginTop: 4,
  },

  // Search loading card
  searchingCard: {
    aspectRatio: 3 / 4,
    borderRadius: 12,
    backgroundColor: 'rgba(142,142,147,0.18)',
    justifyContent: 'center',
    alignItems: 'center',
  },

  // Product metadata (source + price) shown below product card
  productMeta: {
    marginTop: 4,
    gap: 2,
  },
  productMetaText: {
    opacity: 0.7,
  },

  // Detected items grid
  subtitleSection: {
    marginBottom: 16,
    marginTop: 4,
  },
  subtitle: {
    textAlign: 'left',
  },
  description: {
    textAlign: 'left',
    marginTop: 4,
  },
  grid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    marginHorizontal: -8,
  },
  gridItem: {
    width: '50%',
    paddingHorizontal: 8,
    marginBottom: 16,
  },
  buttonsContainer: {
    flexDirection: 'row',
    gap: 12,
    paddingBottom: 24,
    paddingTop: 8,
  },
  backButton: {
    flex: 1,
  },
  nextButton: {
    flex: 1,
  },
});
