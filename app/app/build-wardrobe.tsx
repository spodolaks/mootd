import { useState, useCallback } from 'react';
import { Alert } from 'react-native';
import { useRouter } from 'expo-router';
import * as ImagePicker from 'expo-image-picker';
import { BuildWardrobeScreen } from '@/src/screens';
import { useWardrobeStore } from '@/src/store';
import type { DetectionStep } from '@/src/store/wardrobeStore';
import { wardrobeRepository } from '@/src/data/repositories';
import type { ClothingDetectionResult } from '@/src/domain';

/**
 * Convert API detection result into the DetectionStep[] format the wardrobe
 * store expects. Each detected item becomes one step with a single
 * "similar item" option (the detected item itself).
 */
const toDetectionSteps = (result: ClothingDetectionResult): DetectionStep[] =>
  result.items.map(item => ({
    category: item.category,
    similarItems: [
      {
        id: item.id,
        label: item.label,
        imageSource: item.pngImageUrl
          ? { uri: item.pngImageUrl }
          : item.imageUrl
            ? { uri: item.imageUrl }
            : undefined,
        hasPng: !!item.pngImageUrl,
        // Carry the detected attributes through to the review flow.
        // Without this the onboarding wizard dropped every detected
        // trait (material, color, fit, …) and showed an empty form.
        traits: item.traits,
      },
    ],
  }));

export default function BuildWardrobe() {
  const router = useRouter();
  const { initializeFlow } = useWardrobeStore();
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  /**
   * Shared handler: send the selected image URI to the detection
   * repository (mock or real API) and navigate into the detection flow.
   */
  const processImage = useCallback(
    async (uri: string) => {
      setIsLoading(true);
      setError(null);
      try {
        const result = await wardrobeRepository.detectClothing(uri);
        const steps = toDetectionSteps(result);
        if (steps.length === 0) {
          setError('No clothing items were detected. Try a different photo.');
          return;
        }
        // mootd#161 — mark this as the onboarding flow so trait-selection's
        // Done handler keeps the permissions pitch + completion screen.
        initializeFlow(steps, 'onboarding');
        router.push('/detected-item');
      } catch (e) {
        const message = e instanceof Error ? e.message : 'Something went wrong during detection.';
        setError(message);
      } finally {
        setIsLoading(false);
      }
    },
    [initializeFlow, router]
  );

  const handleTakePhoto = useCallback(async () => {
    // Request camera permission
    const { status } = await ImagePicker.requestCameraPermissionsAsync();
    if (status !== 'granted') {
      Alert.alert('Permission required', 'Camera access is needed to take a photo of your outfit.');
      return;
    }

    const result = await ImagePicker.launchCameraAsync({
      mediaTypes: ['images'],
      // Skip the iOS square-crop editor so the detector gets the full frame.
      quality: 0.8,
    });

    if (!result.canceled && result.assets?.[0]?.uri) {
      await processImage(result.assets[0].uri);
    }
  }, [processImage]);

  const handleUploadFromGallery = useCallback(async () => {
    // Request media library permission
    const { status } = await ImagePicker.requestMediaLibraryPermissionsAsync();
    if (status !== 'granted') {
      Alert.alert('Permission required', 'Photo library access is needed to choose a photo.');
      return;
    }

    const result = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ['images'],
      // Same reason — keep the original resolution, skip the crop UI.
      quality: 0.8,
    });

    if (!result.canceled && result.assets?.[0]?.uri) {
      await processImage(result.assets[0].uri);
    }
  }, [processImage]);

  const handleBrowseCatalog = () => {
    router.replace('/(main)/moodboard');
  };

  return (
    <BuildWardrobeScreen
      onTakePhoto={handleTakePhoto}
      onUploadFromGallery={handleUploadFromGallery}
      onBrowseCatalog={handleBrowseCatalog}
      isLoading={isLoading}
      error={error}
    />
  );
}
