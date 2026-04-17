import { useRouter } from 'expo-router';
import { wardrobeRepository } from '@/src/data/repositories';
import { DetectedItemScreen } from '@/src/screens';
import type { ClothingSearchProduct } from '@/src/domain';
import {
  useWardrobeStore,
  getDefaultTraitsForCategory,
  type WardrobeItem,
} from '@/src/store';

export default function DetectedItem() {
  const router = useRouter();
  const {
    getCurrentDetectionStep,
    currentStepIndex,
    getTotalSteps,
    setItemForStep,
    getItemForStep,
    isFirstStep,
    previousStep,
    reset,
  } = useWardrobeStore();

  const currentStep = getCurrentDetectionStep();
  const totalSteps = getTotalSteps();
  const existingItem = getItemForStep(currentStepIndex);

  const handleExit = () => {
    if (isFirstStep()) {
      reset();
      router.back();
    } else {
      // Go back to previous step's TraitSelection
      previousStep();
      router.back();
    }
  };

  const handleItemSelected = (selectedItemId: string, brand: string) => {
    if (!currentStep) return;

    const selectedItem = currentStep.similarItems.find(
      (item) => item.id === selectedItemId
    );

    if (!selectedItem) return;

    // Check if we already have an item for this step with the same selection
    const isSameSelection =
      existingItem &&
      existingItem.category === currentStep.category &&
      existingItem.label === selectedItem.label;

    if (isSameSelection) {
      // Keep existing item with its traits, just navigate
      router.push('/trait-selection');
      return;
    }

    // Use the backend UUID directly as the item ID so updateItem calls use the correct ID.
    // Build trait list dynamically from ALL detected traits, then fill in any missing defaults.
    const detectedTraits = selectedItem.traits ?? {};
    const defaultTraits = getDefaultTraitsForCategory(currentStep.category);
    const seenIds = new Set<string>();

    // First: create traits from all detected values
    const traitsFromDetection = Object.entries(detectedTraits)
      .filter(([key]) => key !== 'brand') // brand handled separately below
      .map(([key, value]) => {
        seenIds.add(key);
        // Find matching default for display name and options, if any
        const defaultMatch = defaultTraits.find((t) => t.id === key);
        return {
          id: key,
          name: defaultMatch?.name ?? key.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase()),
          selectedValue: value,
          options: defaultMatch?.options ?? [],
        };
      });

    // Then: add any defaults that weren't in the detected traits
    const missingDefaults = defaultTraits
      .filter((t) => !seenIds.has(t.id))
      .map((t) => ({ ...t }));

    const allTraits = [...traitsFromDetection, ...missingDefaults];

    const brandTrimmed = (detectedTraits['brand'] || brand).trim();
    const traitsWithBrand = brandTrimmed
      ? [
          ...allTraits,
          { id: 'brand', name: 'Brand', selectedValue: brandTrimmed, options: [] },
        ]
      : allTraits;

    const wardrobeItem: WardrobeItem = {
      id: selectedItemId,
      category: currentStep.category,
      label: selectedItem.label,
      imageSource: selectedItem.imageSource,
      traits: traitsWithBrand,
    };

    // Save item for this step
    setItemForStep(currentStepIndex, wardrobeItem);

    // Navigate to trait selection
    router.push('/trait-selection');
  };

  // If no current step, show loading or return null
  if (!currentStep) {
    return null;
  }

  // Create single-step data for DetectedItemScreen
  const singleStepData = [
    {
      category: currentStep.category,
      similarItems: currentStep.similarItems,
    },
  ];

  // Find the initial selection if an item already exists for this step
  const getInitialSelection = (): Record<number, string> => {
    if (!existingItem) return {};
    // Find the item in similarItems that matches the existing selection
    const matchingItem = currentStep.similarItems.find(
      (item) => item.label === existingItem.label
    );
    if (matchingItem) {
      return { 0: matchingItem.id };
    }
    return {};
  };

  const handleProductSelected = (product: ClothingSearchProduct, detectedItemId: string, brand: string) => {
    if (!currentStep) return;

    // For product selections, start with defaults (no detected traits available)
    const defaultTraits = getDefaultTraitsForCategory(currentStep.category).map((t) => ({ ...t }));
    const brandTrimmed = brand.trim();
    const traitsWithBrand = brandTrimmed
      ? [
          ...defaultTraits,
          { id: 'brand', name: 'Brand', selectedValue: brandTrimmed, options: [] },
        ]
      : defaultTraits;

    const wardrobeItem: WardrobeItem = {
      id: detectedItemId,
      category: currentStep.category,
      label: product.title,
      imageSource: product.imageUrl ? { uri: product.imageUrl } : undefined,
      productImageUrl: product.imageUrl || undefined,
      traits: traitsWithBrand,
    };

    setItemForStep(currentStepIndex, wardrobeItem);
    router.push('/trait-selection');
  };

  return (
    <DetectedItemScreen
      steps={singleStepData}
      currentStepOverride={currentStepIndex}
      totalStepsOverride={totalSteps}
      initialSelections={getInitialSelection()}
      onExit={handleExit}
      onBrandSearch={(itemId, brand) => wardrobeRepository.searchByBrand(itemId, brand)}
      onProductSelect={handleProductSelected}
      onComplete={(selections, brand) => {
        const selectedItemId = selections[0];
        if (selectedItemId) {
          handleItemSelected(selectedItemId, brand);
        }
      }}
    />
  );
}
