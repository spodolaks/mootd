import { useRouter } from 'expo-router';
import { LoadingScreen } from '@/src/screens';
import { useWardrobeStore } from '@/src/store';

export default function Loading() {
  const router = useRouter();
  const { reset } = useWardrobeStore();

  const handleComplete = () => {
    // All done - reset wardrobe state and go to moodboard screen with tab navigation
    reset();
    router.dismissAll();
    router.replace('/(main)/moodboard');
  };

  return (
    <LoadingScreen
      text="Generating"
      duration={3000}
      onComplete={handleComplete}
    />
  );
}
