import { useRouter } from 'expo-router';
import { PermissionsScreen } from '@/src/screens';

export default function Permissions() {
  const router = useRouter();

  const handleGetStarted = () => {
    // Navigate to loading screen
    router.push('/loading');
  };

  return (
    <PermissionsScreen
      onGetStarted={handleGetStarted}
    />
  );
}
