import { useRouter } from 'expo-router';
import { Alert } from 'react-native';
import { TraitSelectionScreen } from '@/src/screens';
import { useWardrobeStore } from '@/src/store';
import { wardrobeRepository } from '@/src/data/repositories';

export default function TraitSelection() {
  const router = useRouter();
  const { getAllItems } = useWardrobeStore();

  const handleBack = () => {
    router.back();
  };

  const handleNextItem = () => {
    router.push('/detected-item');
  };

  const handleComplete = async () => {
    const allItems = getAllItems();
    try {
      await Promise.all(
        allItems.map(item => {
          const traitsMap = Object.fromEntries(
            item.traits
              .filter(t => t.selectedValue && t.selectedValue.trim() !== '')
              .map(t => [t.id, t.selectedValue as string])
          );
          return wardrobeRepository.updateItem(
            item.id,
            traitsMap,
            item.label,
            item.productImageUrl
          );
        })
      );
    } catch (e) {
      Alert.alert('Error', e instanceof Error ? e.message : 'Failed to save items.');
      return;
    }
    router.push('/permissions');
  };

  return (
    <TraitSelectionScreen
      onBack={handleBack}
      onNextItem={handleNextItem}
      onComplete={() => {
        void handleComplete();
      }}
    />
  );
}
