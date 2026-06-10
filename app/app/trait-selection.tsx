import { useRouter } from 'expo-router';
import { Alert } from 'react-native';
import { TraitSelectionScreen } from '@/src/screens';
import { useWardrobeStore, useUIStore } from '@/src/store';
import { wardrobeRepository } from '@/src/data/repositories';

export default function TraitSelection() {
  const router = useRouter();
  const getAllItems = useWardrobeStore(s => s.getAllItems);
  const flowOrigin = useWardrobeStore(s => s.flowOrigin);
  const reset = useWardrobeStore(s => s.reset);
  const showToast = useUIStore(s => s.showToast);

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

    // mootd#161 — the detection → review wizard is shared by onboarding and
    // the in-app "add item" flow on the Wardrobe tab. Branch on where the
    // flow started:
    //   - onboarding: keep the existing tail (permissions "get notified"
    //     pitch → completion screen).
    //   - in-app add: the user already has a wardrobe and isn't being
    //     onboarded, so skip the permissions pitch AND the fake fixed-duration
    //     "Generating" loading screen. Clear the wizard state, return to the
    //     Wardrobe tab, and confirm with a toast.
    if (flowOrigin === 'add') {
      const count = allItems.length;
      reset();
      router.dismissAll();
      router.replace('/(main)/wardrobe');
      showToast(`Added ${count} item${count === 1 ? '' : 's'} to your wardrobe`, 'success');
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
